package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"

	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/kcoddington/chirpy/internal/database"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileServerHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
}

func (a *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		a.fileServerHits.Add(1)
	})
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func convertDbUserToUser(user database.User) User {
	return User{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}

func responseWithJSON(w http.ResponseWriter, code int, payload interface{}) error {
	resp, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	w.Header().Add("content-type", "application/json")
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	w.Write(resp)
	return nil
}

func respondWithError(w http.ResponseWriter, code int, msg string) error {
	return responseWithJSON(w, code, map[string]string{"error": msg})
}

func badWordReplacement(badWords []string, body *string) string {
	bodySplit := strings.Split(*body, " ")
	for i, word := range bodySplit {
		if slices.Contains(badWords, strings.ToLower(word)) {
			bodySplit[i] = "****"
		}
	}
	*body = strings.Join(bodySplit, " ")
	return *body
}

func main() {
	apiCfg := apiConfig{}
	dbURL := os.Getenv("POSTGRES_CONN_CHIRPY")
	godotenv.Load()
	apiCfg.platform = os.Getenv("PLATFORM")
	if apiCfg.platform == "" {
		log.Fatalf("PLATFORM environment variable is not set")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	mux := http.NewServeMux()
	apiCfg.dbQueries = database.New(db)
	fileServerHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fileServerHandler))
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			Email string `json:"email"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		dbUser, err := apiCfg.dbQueries.CreateUser(r.Context(), p.Email)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 201, convertDbUserToUser(dbUser))
	})
	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			Body string `json:"body"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		if len(p.Body) > 140 {
			respondWithError(w, 400, "Chirp is too long")
			return
		}
		badWordReplacement([]string{"kerfuffle", "sharbert", "fornax"}, &p.Body)
		responseWithJSON(w, 200, map[string]string{"cleaned_body": p.Body})
	})
	mux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("content-type", "text/html; charset=utf-8")
		metricsHtml := fmt.Sprintf(`
		<html>
			  <body>
			<h1>Welcome, Chirpy Admin</h1>
			<p>Chirpy has been visited %d times!</p>
		</body>
		</html>
	`, apiCfg.fileServerHits.Load())
		w.Write([]byte(metricsHtml))
	})
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("content-type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		if apiCfg.platform != "dev" {
			respondWithError(w, 403, "Forbidden")
			return
		}
		apiCfg.fileServerHits.Swap(0)
		apiCfg.dbQueries.DeleteAllUsers(r.Context())
	})
	server := http.Server{}
	server.Handler = mux
	server.Addr = ":8080"
	if err := server.ListenAndServe(); err != nil {
		return
	}
}
