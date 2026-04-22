package main

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	"github.com/kcoddington/chirpy/internal"
	"github.com/kcoddington/chirpy/internal/database"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileServerHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
	tokenSecret    string
	polkaAPIKey    string
}

func (a *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		a.fileServerHits.Add(1)
	})
}

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	IsChirpyRed  bool      `json:"is_chirpy_red"`
}

func convertDbUserToUser(user database.User) User {
	return User{
		ID:          user.ID,
		Email:       user.Email,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		IsChirpyRed: user.IsChirpyRed,
	}
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    uuid.UUID `json:"user_id"`
}

func convertDbChirpsToChirps(dbChirps []database.Chirp) []Chirp {
	out := make([]Chirp, len(dbChirps))
	for i := range dbChirps {
		out[i] = convertDbChirpToChirp(dbChirps[i])
	}
	return out
}

func convertDbChirpToChirp(chirp database.Chirp) Chirp {
	return Chirp{
		ID:        chirp.ID,
		Body:      chirp.Body,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		UserID:    chirp.UserID,
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

func badWordReplacement(badWords []string, body *string) {
	bodySplit := strings.Split(*body, " ")
	for i, word := range bodySplit {
		if slices.Contains(badWords, strings.ToLower(word)) {
			bodySplit[i] = "****"
		}
	}
	*body = strings.Join(bodySplit, " ")
}

func main() {
	apiCfg := apiConfig{}
	dbURL := os.Getenv("POSTGRES_CONN_CHIRPY")
	if dbURL == "" {
		log.Fatalf("POSTGRES_CONN_CHIRPY environment variable is not set")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

	tokenSecret := os.Getenv("CHIRPY_JWT_SIGNING_KEY")
	if tokenSecret == "" {
		log.Fatalf("CHIRPY_JWT_SIGNING_KEY environment variable is not set")
	}
	apiCfg.tokenSecret = tokenSecret

	godotenv.Load()
	apiCfg.platform = os.Getenv("PLATFORM")
	if apiCfg.platform == "" {
		log.Fatalf("PLATFORM environment variable is not set")
	}
	apiCfg.polkaAPIKey = os.Getenv("POLKA_API_KEY")
	if apiCfg.polkaAPIKey == "" {
		log.Fatalf("POLKA_API_KEY environment variable is not set")
	}

	mux := http.NewServeMux()
	apiCfg.dbQueries = database.New(db)
	fileServerHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fileServerHandler))

	// USER ROUTES
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		hashedPassword, err := internal.HashPassword(p.Password)
		dbUser, err := apiCfg.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
			Email:          p.Email,
			HashedPassword: hashedPassword,
		})
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 201, convertDbUserToUser(dbUser))
	})
	mux.HandleFunc("PUT /api/users", func(w http.ResponseWriter, r *http.Request) {
		token, err := internal.GetBearerToken(r.Header)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		userUUID, err := internal.ValidateJWT(token, apiCfg.tokenSecret)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		type params struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		// update user email and/or password
		var hashedPassword string
		dbUser, err := apiCfg.dbQueries.GetUserByID(r.Context(), userUUID)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		if p.Email != "" {
			dbUser.Email = p.Email
		}
		if p.Password != "" {
			hashedPassword, err := internal.HashPassword(p.Password)
			if err != nil {
				respondWithError(w, 500, err.Error())
				return
			}
			dbUser.HashedPassword = hashedPassword
		}
		if hashedPassword != "" {
			hashedPassword, err = internal.HashPassword(p.Password)
			if err != nil {
				respondWithError(w, 500, err.Error())
				return
			}
		}
		dbUser, err = apiCfg.dbQueries.UpdateUser(r.Context(), database.UpdateUserParams{
			ID:             dbUser.ID,
			Email:          p.Email,
			HashedPassword: hashedPassword,
		})
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseUser := convertDbUserToUser(dbUser)
		responseUser.Token = token
		responseUser.RefreshToken = internal.MakeRefreshToken()
		responseWithJSON(w, 200, responseUser)
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 400, err.Error())
			return
		}

		dbUser, err := apiCfg.dbQueries.GetUserByEmail(r.Context(), p.Email)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}

		isGood, err := internal.CheckPasswordHash(p.Password, dbUser.HashedPassword)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		if !isGood {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		token, err := internal.MakeJWT(dbUser.ID, apiCfg.tokenSecret)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		user := convertDbUserToUser(dbUser)
		user.Token = token
		user.RefreshToken = internal.MakeRefreshToken()
		_, err = apiCfg.dbQueries.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
			Token:     user.RefreshToken,
			UserID:    user.ID,
			ExpiresAt: time.Now().Add(60 * 24 * time.Hour),
			RevokedAt: sql.NullTime{},
		})
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 200, user)
	})
	mux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			RefreshToken string `json:"refresh_token"`
		}
		refreshToken, err := internal.GetBearerToken(r.Header)
		if err != nil || refreshToken == "" {
			var p params
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.RefreshToken == "" {
				respondWithError(w, 400, "Missing or invalid refresh token")
				return
			}
			refreshToken = p.RefreshToken
		}
		dbRefreshToken, err := apiCfg.dbQueries.GetRefreshTokenByToken(r.Context(), refreshToken)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		if dbRefreshToken.ExpiresAt.Before(time.Now()) {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		if dbRefreshToken.RevokedAt.Valid {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		dbUser, err := apiCfg.dbQueries.GetUserFromRefreshToken(r.Context(), dbRefreshToken.Token)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		newToken, err := internal.MakeJWT(dbUser.ID, apiCfg.tokenSecret)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 200, map[string]string{"token": newToken})
	})
	mux.HandleFunc("POST /api/revoke", func(w http.ResponseWriter, r *http.Request) {
		refreshToken, err := internal.GetBearerToken(r.Header)
		if err != nil || refreshToken == "" {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		err = apiCfg.dbQueries.RevokeRefreshToken(r.Context(), refreshToken)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 204, nil)
	})

	// POLKA WEBHOOK ROUTES
	mux.HandleFunc("POST /api/polka/webhooks", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			Event string `json:"event"`
			Data  struct {
				UserID string `json:"user_id"`
			} `json:"data"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		if p.Event != "user.upgraded" {
			responseWithJSON(w, 204, nil)
			return
		}
		// TODO - check API key from service to verify request - not implemented yet
		apiKey, err := internal.GetPolkaAPIKey(r.Header)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		if apiKey != apiCfg.polkaAPIKey {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		_, err = apiCfg.dbQueries.UpdateUserIsChirpyRed(r.Context(), database.UpdateUserIsChirpyRedParams{
			ID:          uuid.MustParse(p.Data.UserID),
			IsChirpyRed: true,
		})
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 204, nil)
	})

	// CHIRP ROUTES
	mux.HandleFunc("GET /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		// check for query param "author_id" that maps to chirp user_id
		authorID := r.URL.Query().Get("author_id")
		var dbChirps []database.Chirp
		var err error
		if authorID != "" {
			dbChirps, err = apiCfg.dbQueries.GetChirpsByUserID(r.Context(), uuid.MustParse(authorID))
		} else {
			dbChirps, err = apiCfg.dbQueries.GetChirps(r.Context())
		}
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 200, convertDbChirpsToChirps(dbChirps))
	})
	mux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		type params struct {
			Body   string    `json:"body"`
			UserID uuid.UUID `json:"user_id"`
		}
		var p params
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		token, err := internal.GetBearerToken(r.Header)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		userUUID, err := internal.ValidateJWT(token, apiCfg.tokenSecret)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		p.UserID = userUUID
		if len(p.Body) > 140 {
			respondWithError(w, 400, "Chirp is too long")
			return
		}
		badWordReplacement([]string{"kerfuffle", "sharbert", "fornax"}, &p.Body)
		dbChirp, err := apiCfg.dbQueries.CreateChirp(r.Context(), database.CreateChirpParams{
			Body:   p.Body,
			UserID: p.UserID,
		})
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 201, convertDbChirpToChirp(dbChirp))

	})
	mux.HandleFunc("GET /api/chirps/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		idUUID, err := uuid.Parse(id)
		if err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		dbChirp, err := apiCfg.dbQueries.GetChirpByID(r.Context(), idUUID)
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, 404, "Chirp not found")
			return
		}
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		responseWithJSON(w, 200, convertDbChirpToChirp(dbChirp))
	})
	mux.HandleFunc("DELETE /api/chirps/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		idUUID, err := uuid.Parse(id)
		if err != nil {
			respondWithError(w, 400, err.Error())
			return
		}
		// validate JWT and authz
		token, err := internal.GetBearerToken(r.Header)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		userUUID, err := internal.ValidateJWT(token, apiCfg.tokenSecret)
		if err != nil {
			respondWithError(w, 401, "Unauthorized")
			return
		}
		dbChirp, err := apiCfg.dbQueries.GetChirpByID(r.Context(), idUUID)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		if dbChirp.UserID != userUUID {
			respondWithError(w, 403, "Forbidden")
			return
		}

		err = apiCfg.dbQueries.DeleteChirp(r.Context(), idUUID)
		if err != nil {
			respondWithError(w, 500, err.Error())
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, 404, "Chirp not found")
			return
		}
		responseWithJSON(w, 204, nil)
	})

	// ADMIN ROUTES
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
	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		if apiCfg.platform != "dev" {
			respondWithError(w, 403, "Forbidden")
			return
		}
		apiCfg.fileServerHits.Swap(0)
		apiCfg.dbQueries.DeleteAllUsers(r.Context())
	})

	// HEALTHZ ROUTE
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("content-type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

	server := http.Server{}
	server.Handler = mux
	server.Addr = ":8080"
	if err := server.ListenAndServe(); err != nil {
		return
	}
}
