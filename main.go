package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
)

type apiConfig struct {
	fileServerHits atomic.Int32
}

func (a *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		a.fileServerHits.Add(1)
	})
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
	mux := http.NewServeMux()
	apiCfg := apiConfig{}
	fileServerHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fileServerHandler))
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
		apiCfg.fileServerHits.Swap(0)
	})
	server := http.Server{}
	server.Handler = mux
	server.Addr = ":8080"
	if err := server.ListenAndServe(); err != nil {
		return
	}
}
