package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

var usernameRE = regexp.MustCompile(`^[A-Za-z]+$`)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    username   TEXT PRIMARY KEY,
    birthdate  DATE NOT NULL
);
`

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}
	return db, nil
}

func upsertUser(db *sql.DB, username string, birth time.Time) error {
	_, err := db.Exec(`
	INSERT INTO users (username, birthdate) VALUES ($1, $2)
	ON CONFLICT (username) DO UPDATE SET birthdate = EXCLUDED.birthdate;
	`, username, birth)
	return err
}

func getUserBirth(db *sql.DB, username string) (time.Time, error) {
	var birth time.Time
	err := db.QueryRow(`SELECT birthdate FROM users WHERE username = $1`, username).Scan(&birth)
	return birth, err
}

type putPayload struct {
	DateOfBirth string `json:"dateOfBirth"`
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

func daysUntil(birth time.Time) int {
	now := time.Now()
	thisYear := time.Date(now.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, now.Location())
	if thisYear.Before(now) {
		thisYear = thisYear.AddDate(1, 0, 0)
	}
	return int(thisYear.Sub(now).Hours() / 24)
}

func putHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.Param("username")
		if !usernameRE.MatchString(username) {
			c.String(http.StatusBadRequest, "username must contain only letters")
			return
		}

		var p putPayload
		if err := c.ShouldBindJSON(&p); err != nil {
			c.String(http.StatusBadRequest, "invalid JSON")
			return
		}
		birth, err := parseDate(p.DateOfBirth)
		if err != nil {
			c.String(http.StatusBadRequest, "invalid date format, use YYYY-MM-DD")
			return
		}
		if birth.After(time.Now()) {
			c.String(http.StatusBadRequest, "date of birth must be before today")
			return
		}

		if err := upsertUser(db, username, birth); err != nil {
			c.String(http.StatusInternalServerError, "database error")
			return
		}
		c.Status(http.StatusNoContent) // 204
	}
}

func getHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.Param("username")
		birth, err := getUserBirth(db, username)
		if err == sql.ErrNoRows {
			c.String(http.StatusNotFound, "user not found")
			return
		}
		if err != nil {
			c.String(http.StatusInternalServerError, "database error")
			return
		}

		now := time.Now()
		bdayThisYear := time.Date(now.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, now.Location())
		var msg string
		if now.Month() == bdayThisYear.Month() && now.Day() == bdayThisYear.Day() {
			msg = fmt.Sprintf("Hello, %s! Happy birthday!", username)
		} else {
			days := daysUntil(birth)
			msg = fmt.Sprintf("Hello, %s! Your birthday is in %d day(s)", username, days)
		}
		c.JSON(http.StatusOK, gin.H{"message": msg})
	}
}

// Liveness probe – always returns 200 if the process is running
func livenessHandler(c *gin.Context) {
	c.Status(http.StatusOK)
}

// Readiness probe – checks DB connectivity
func readinessHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Ping(); err != nil {
			c.String(http.StatusServiceUnavailable, "database unreachable")
			return
		}
		c.Status(http.StatusOK)
	}
}

func main() {
	const dsnKey = "POSTGRES_DSN"
	dsn := os.Getenv(dsnKey)
	val, ok := os.LookupEnv(dsnKey)
	if !ok || val == "" {
		log.Fatalf("environment variable %s is not set", dsnKey)
	}

	db, err := openDB(dsn)
	if err != nil {
		log.Fatalf("failed to connect DB: %v", err)
	}
	defer db.Close()

	router := gin.Default()

	// API endpoints
	router.PUT("/hello/:username", putHandler(db))
	router.GET("/hello/:username", getHandler(db))

	// Health endpoints
	router.GET("/livez", livenessHandler)       // liveness
	router.GET("/readyz", readinessHandler(db)) // readiness

	addr := ":8080"
	log.Printf("Gin service listening on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("router error: %v", err)
	}
}
