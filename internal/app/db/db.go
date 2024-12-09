package db

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"strconv"
	"strings"
)

type Song struct {
	Group string `json:"group,omitempty"`
	Name  string `json:"song,omitempty"`
	SongDetail
}

type SongDetail struct {
	ReleaseDate string `json:"releaseDate,omitempty"`
	Text        string `json:"text,omitempty"`
	Link        string `json:"link,omitempty"`
}

type Library []Song

type Database struct {
	config *Config
	dbConn *pgxpool.Pool
}

func New(config *Config) *Database {
	return &Database{config: config}
}

func (db *Database) Open() error {
	dbConn, err := pgxpool.New(context.Background(), db.config.ConnString())
	if err != nil {
		return err
	}

	err = dbConn.Ping(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), `database "`+db.config.DbName+`" does not exist`) {
			err := createDB(dbConn, db.config.DbName)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	db.dbConn = dbConn

	err = db.checkDBVersion()
	if err != nil {
		return err
	}

	return nil
}

func (db *Database) Close() {
	db.Close()
	return
}

func createDB(dbConn *pgxpool.Pool, dbname string) error {
	// подразумеваем, что база данных создана администратором СУБД,
	// имеющим соответствующие привилегии
	return errors.New("database doesn't exist")
}

func (db *Database) migrateToCurrentVersion() error {

	const queryCreateTable = `drop table if exists music_library;
create table music_library (
author varchar(40),
song varchar(40),
releaseDate varchar(10),
song_text text,
link varchar(40),
primary key (author, song))`

	_, err := db.dbConn.Exec(context.Background(), queryCreateTable)
	if err != nil {
		return err
	}

	const queryCreateVersion = `drop table if exists version;
create table version (version integer)`

	_, err = db.dbConn.Exec(context.Background(), queryCreateVersion)
	if err != nil {
		return err
	}

	const queryUpdateVersion = `insert into version (version) values (1)`

	_, err = db.dbConn.Exec(context.Background(), queryUpdateVersion)
	if err != nil {
		return err
	}

	return nil
}

func (db *Database) checkDBVersion() error {
	// проверяем текущую версию БД
	row := db.dbConn.QueryRow(context.Background(), `select version from version limit 1`)
	var version int
	err := row.Scan(&version)
	if err != nil && !strings.Contains(err.Error(), `relation "version" does not exist`) {
		return err
	}

	if version != 1 {
		// Версия структуры БД некорректна - пересоздаём
		err = db.migrateToCurrentVersion()
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *Database) ListAllLibrary(fp Song, offset, limit string) (Library, error) {
	q := `select * from music_library where (
		($1 = '' or author = $1) and
		($2 = '' or song = $2) and
		($3 = '' or releasedate = $3) and
		($4 = '' or song_text = $4) and
		($5 = '' or link = $5)) order by author, song`
	if offset != "" {
		offsetInt, err := strconv.Atoi(offset)
		if err != nil {
			return nil, err
		}
		q = q + fmt.Sprintf(" offset %d", offsetInt)
	}
	if limit != "" {
		limitInt, err := strconv.Atoi(limit)
		if err != nil {
			return nil, err
		}
		q = q + fmt.Sprintf(" limit %d", limitInt)
	}
	slog.Debug("list all library database query", "filter params", fp, "offset", offset, "limit", limit)
	rows, err := db.dbConn.Query(context.Background(), q, fp.Group, fp.Name, fp.ReleaseDate, fp.Text, fp.Link)
	defer rows.Close()
	if err != nil {
		return nil, err
	}

	s := Song{}
	lib := make(Library, 0, 64)

	for rows.Next() {
		err = rows.Scan(&s.Group, &s.Name, &s.ReleaseDate, &s.Text, &s.Link)
		if err != nil {
			return nil, err
		}
		lib = append(lib, s)
	}
	return lib, nil
}

func (db *Database) DeleteSong(author, songName string) (string, error) {
	tag, err := db.dbConn.Exec(context.Background(), `delete from music_library where (
    author, song) = ($1, $2)`, author, songName)
	slog.Debug("deleting from DB", "db response", tag.String())
	if err != nil {
		return "", err
	}

	return tag.String(), nil
}

func (db *Database) AddSong(s Song) error {
	tag, err := db.dbConn.Exec(context.Background(), `insert into music_library (author,song,releasedate,song_text,link) 
values ($1,$2,$3,$4,$5)`, s.Group, s.Name, s.ReleaseDate, s.Text, s.Link)
	slog.Debug("adding to DB", "db response", tag.String())
	if err != nil {
		return err
	}
	return nil
}

func (db *Database) UpdateSong(s Song) error {
	tag, err := db.dbConn.Exec(context.Background(), `update music_library
set releasedate=$1,
    song_text=$2,
    link=$3
where author=$4 and song=$5`, s.ReleaseDate, s.Text, s.Link, s.Group, s.Name)
	slog.Debug("updating in DB", "db response", tag.String())
	if err != nil {
		return err
	}
	return nil
}

func (db *Database) GetSongText(author, songName string) (string, error) {
	row := db.dbConn.QueryRow(context.Background(), `select song_text from music_library where (
    author, song) = ($1, $2)`, author, songName)
	var t string
	err := row.Scan(&t)
	if err != nil {
		slog.Error("error retrieving from db", "error", err.Error())
		return "", err
	}

	return t, nil
}
