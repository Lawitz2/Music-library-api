package apiserver

import (
	"ApiServer/internal/app/db"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type APIServer struct {
	config   *Config
	router   *mux.Router
	database *db.Database
}

func NewAPIServer(config *Config) *APIServer {
	return &APIServer{
		config: config,
		router: mux.NewRouter(),
	}
}

func (s *APIServer) Start() error {
	slog.Debug("debug is enabled")

	s.configureRouter()

	err := s.configureDB()
	if err != nil {
		return err
	}

	slog.Info("starting api server")

	return http.ListenAndServe(s.config.BindAddr, s.router)
}

func parseLevel(s string) (slog.Level, error) {
	var level slog.Level
	err := level.UnmarshalText([]byte(s))
	return level, err
}

func (s *APIServer) configureRouter() {
	s.router.HandleFunc("/library/all", s.listLibrary()).Methods("GET")
	s.router.HandleFunc("/library/text", s.showSongText()).Methods("GET")
	s.router.HandleFunc("/library/delete", s.deleteSong()).Methods("DELETE")
	s.router.HandleFunc("/library/add", s.addSong()).Methods("POST")
	s.router.HandleFunc("/library/update", s.updateSong()).Methods("PUT")
}

func (s *APIServer) configureDB() error {
	slog.Debug("Database connection string: " + s.config.Database.ConnString())
	database := db.New(s.config.Database)
	err := database.Open()
	if err != nil {
		slog.Error(err.Error())
		return err
	}

	s.database = database
	return nil
}

// функция парсит параметры запроса и выдаёт отфильтрованный
// на их основе лист песен
// если параметр не указан - фильтрация по нему не происходит
func (s *APIServer) listLibrary() http.HandlerFunc {
	var lib db.Library
	var filterParams db.Song
	var offset, limit string
	var err error
	return func(writer http.ResponseWriter, request *http.Request) {
		slog.Info("list library request", "from", request.RemoteAddr, "to", request.Host+request.URL.String())
		writer.Header().Set("Content-type", "application/json")

		filterParams.Group = request.FormValue("author")
		filterParams.Name = request.FormValue("song")
		filterParams.ReleaseDate = request.FormValue("releaseDate")
		filterParams.Text = request.FormValue("text")
		filterParams.Link = request.FormValue("link")
		offset = request.FormValue("offset")
		limit = request.FormValue("limit")

		slog.Debug("filter parameters", "struct", filterParams, "offset", offset, "limit", limit)

		lib, err = s.database.ListAllLibrary(filterParams, offset, limit)
		if err != nil {
			writer.WriteHeader(500)
			slog.Error("error retrieving from db", "error", err.Error())
			return
		}

		if len(lib) == 0 {
			writer.WriteHeader(404)
			slog.Debug("song not found", "provided url", request.URL)
			return
		} else {
			encoder := json.NewEncoder(writer)
			for _, val := range lib {
				encoder.Encode(val)
			}
		}
	}
}

func (s *APIServer) deleteSong() http.HandlerFunc {
	var author, song string

	return func(writer http.ResponseWriter, request *http.Request) {
		slog.Info("delete song request", "from", request.RemoteAddr, "to", request.Host+request.URL.String())
		// необходимы оба поля author и song для точного определения песни,
		// которую необходимо удалить
		if author == "" || song == "" {
			slog.Error("bad request, author and/or name of the song weren't provided", "request", request.Host+request.URL.String())
			writer.WriteHeader(400)
			return
		}

		author = request.FormValue("author")
		song = request.FormValue("song")
		slog.Debug("delete request", "author", author, "song", song)

		err := s.database.DeleteSong(author, song)
		if err != nil {
			slog.Error("error deleting from database", "error", err.Error())
			writer.WriteHeader(500)
		}
	}
}

func (s *APIServer) showSongText() http.HandlerFunc {
	var author, song, text, verse string
	var err error

	return func(writer http.ResponseWriter, request *http.Request) {
		slog.Info("song text request", "from", request.RemoteAddr, "to", request.Host+request.URL.String())
		author = request.FormValue("author")
		song = request.FormValue("song")
		verse = request.FormValue("verse")

		slog.Debug("", "author", author, "song", song, "verse", verse)

		// необходимы оба поля author и song для точного определения песни,
		// текст которой необходимо показать
		if author == "" || song == "" {
			slog.Error("bad request, author and/or name of the song weren't provided", "request", request.Host+request.URL.String())
			writer.WriteHeader(400)
			return
		}

		// подразумеваем, что куплеты песни разделены между собой
		// одной пустой строкой
		text, err = s.database.GetSongText(author, song)
		slog.Debug("", "text", text)
		if err != nil {
			slog.Error("error retrieving from db", "error", err.Error())
			writer.WriteHeader(500)
			return
		}
		tmp := strings.Split(text, "\n\n")

		// если параметр verse не указан (или указан как 0) - выводим весь текст
		// в другом случае выводим указанный куплет (1 = первый куплет, и т.д.)
		// отрицательное значение куплета, а также значение, превышающее кол-во
		// куплетов в песне = bad request
		if verse == "" {
			fmt.Fprint(writer, text)
		} else {
			verseInt, err := strconv.Atoi(verse)
			if err != nil {
				slog.Error(err.Error())
				writer.WriteHeader(400)
				return
			}
			if verseInt > len(tmp) || verseInt < 0 {
				slog.Error("bad request", "verse", verseInt)
				writer.WriteHeader(400)
				return
			}
			if verseInt == 0 {
				fmt.Fprint(writer, text)
			} else {
				fmt.Fprint(writer, tmp[verseInt-1])
			}
		}
	}
}

func (s *APIServer) addSong() http.HandlerFunc {
	var song db.Song
	var body []byte
	var err error
	var reqURL string
	var resp *http.Response
	externalUrl := os.Getenv("EXTERNAL_API_URL")

	return func(writer http.ResponseWriter, request *http.Request) {
		slog.Info("add song request", "from", request.RemoteAddr, "to", request.Host+request.URL.String())
		body, err = io.ReadAll(request.Body)
		defer request.Body.Close()
		if err != nil {
			slog.Error("error reading request body", "error", err.Error())
			writer.WriteHeader(400)
			return
		}

		err = json.Unmarshal(body, &song)
		if err != nil {
			writer.WriteHeader(400)
			slog.Error("error unmarshalling", "error", err.Error())
			return
		}
		slog.Debug("request body", "struct", song)

		reqURL = fmt.Sprintf("%s?group=%s&song=%s", externalUrl, song.Group, song.Name)
		slog.Debug("accessing external api", "url", reqURL)
		timer := time.Second

		// повторяем запрос вплоть до 5 раз в случае получения кода 500
		// в других случаях либо мы получили что и хотели, либо ошибка на нашей стороне,
		// либо ошибка нам неизвестна
	outer:
		for range 5 {
			resp, err = http.Get(reqURL)
			if err != nil {
				writer.WriteHeader(500)
				slog.Error("http.get error", "error", err.Error())
				fmt.Fprint(writer, err.Error())
				return
			}
			switch resp.StatusCode {
			case 400:
				slog.Error("received code 400, bad request")
				writer.WriteHeader(400)
				return
			case 500:
				slog.Debug("received code 500, trying to get " + reqURL + " again")
				time.Sleep(timer)
				timer = min(timer*2, time.Second*10)
			case 200:
				break outer
			default:
				// исходя из ТЗ мы никогда не должны сюда попасть
				writer.WriteHeader(resp.StatusCode)
				slog.Error("got unsupported response code", "code", resp.StatusCode)
				return
			}
		}

		slog.Debug("response from external api", "resp code", resp.StatusCode)

		if resp.StatusCode == 500 {
			slog.Error("external api is not working")
			writer.WriteHeader(500)
			fmt.Fprint(writer, "external api is not working")
			return
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("error reading external api response body", "error", err.Error())
			return
		}
		err = json.Unmarshal(data, &song.SongDetail)
		if err != nil {
			slog.Error("error unmarshalling", "error", err.Error())
			return
		}
		slog.Debug("adding song to database", "song struct", song)
		err = s.database.AddSong(song)
		if err != nil {
			slog.Error("error adding to the database", "error", err.Error())
			writer.WriteHeader(500)
		}
	}
}

func (s *APIServer) updateSong() http.HandlerFunc {
	var song db.Song
	var body []byte
	var err error
	return func(writer http.ResponseWriter, request *http.Request) {
		slog.Info("update song request", "from", request.RemoteAddr, "to", request.Host+request.URL.String())
		song.Group = request.FormValue("author")
		song.Name = request.FormValue("song")

		// необходимы оба поля author и song для точного определения песни,
		// данные которой необходимо обновить
		if song.Group == "" || song.Name == "" {
			slog.Error("bad request, author and/or name of the song weren't provided", "request", request.Host+request.URL.String())
			writer.WriteHeader(400)
			return
		}

		slog.Debug("update", "author", song.Group, "song name", song.Name)

		body, err = io.ReadAll(request.Body)
		if err != nil {
			slog.Error("error reading request body", "error", err.Error())
			writer.WriteHeader(400)
			return
		}

		err = json.Unmarshal(body, &song.SongDetail)
		if err != nil {
			slog.Error("unmarshal error", "error", err.Error())
			writer.WriteHeader(400)
			return
		}
		slog.Debug("", "update body", song.SongDetail)

		err = s.database.UpdateSong(song)
		if err != nil {
			slog.Error("updating database error", "error", err.Error())
			writer.WriteHeader(500)
			return
		}
	}
}