package internal_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kcoddington/chirpy/internal"
)

type userResp struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

type refreshResp struct {
	Token string `json:"token"`
}

func TestRefreshTokenFlow_Integration(t *testing.T) {
	baseURL := os.Getenv("CHIRPY_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration tests")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	email := fmt.Sprintf("refresh-it-%d@example.com", time.Now().UnixNano())
	password := "supersecret123"

	// 1) Create user
	createStatus, _ := postJSON(t, client, baseURL+"/api/users", map[string]string{
		"email":    email,
		"password": password,
	})
	if createStatus != http.StatusCreated {
		t.Fatalf("expected 201 from /api/users, got %d", createStatus)
	}

	// 2) Login and capture refresh token
	loginStatus, loginBody := postJSON(t, client, baseURL+"/api/login", map[string]string{
		"email":    email,
		"password": password,
	})
	if loginStatus != http.StatusOK {
		t.Fatalf("expected 200 from /api/login, got %d body=%s", loginStatus, string(loginBody))
	}

	var login userResp
	if err := json.Unmarshal(loginBody, &login); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if login.RefreshToken == "" {
		t.Fatal("expected refresh_token in login response")
	}
	if login.Token == "" {
		t.Fatal("expected access token in login response")
	}

	// 3) Refresh access token
	refreshStatus, refreshBody := postJSON(t, client, baseURL+"/api/refresh", map[string]string{
		"refresh_token": login.RefreshToken,
	})
	if refreshStatus != http.StatusOK {
		t.Fatalf("expected 200 from /api/refresh, got %d body=%s", refreshStatus, string(refreshBody))
	}
	var refreshed refreshResp
	if err := json.Unmarshal(refreshBody, &refreshed); err != nil {
		t.Fatalf("failed to decode refresh response: %v", err)
	}
	if refreshed.Token == "" {
		t.Fatal("expected refreshed access token")
	}
	tokenSecret := os.Getenv("CHIRPY_JWT_SIGNING_KEY")
	if tokenSecret == "" {
		t.Fatal("CHIRPY_JWT_SIGNING_KEY must be set for integration tests")
	}
	refreshedUserID, err := internal.ValidateJWT(refreshed.Token, tokenSecret)
	if err != nil {
		t.Fatalf("expected refreshed token to be valid, got error: %v", err)
	}
	loginUserID, err := uuid.Parse(login.ID)
	if err != nil {
		t.Fatalf("failed to parse login user id: %v", err)
	}
	if refreshedUserID != loginUserID {
		t.Fatalf("expected refreshed token subject %s, got %s", loginUserID, refreshedUserID)
	}

	// 4) Revoke refresh token (Authorization = raw token per current handler)
	revokeReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/revoke", nil)
	if err != nil {
		t.Fatalf("failed creating revoke request: %v", err)
	}
	revokeReq.Header.Set("Authorization", login.RefreshToken)
	revokeResp, err := client.Do(revokeReq)
	if err != nil {
		t.Fatalf("failed calling /api/revoke: %v", err)
	}
	defer revokeResp.Body.Close()

	if revokeResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(revokeResp.Body)
		t.Fatalf("expected 204 from /api/revoke, got %d body=%s", revokeResp.StatusCode, string(body))
	}

	// 5) Refresh should now fail
	refreshStatus2, refreshBody2 := postJSON(t, client, baseURL+"/api/refresh", map[string]string{
		"refresh_token": login.RefreshToken,
	})
	if refreshStatus2 != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revoke, got %d body=%s", refreshStatus2, string(refreshBody2))
	}
}

func TestRefreshToken_MissingBodyField_Integration(t *testing.T) {
	baseURL := os.Getenv("CHIRPY_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration tests")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	status, body := postJSON(t, client, baseURL+"/api/refresh", map[string]string{})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing refresh_token, got %d body=%s", status, string(body))
	}
}

func postJSON(t *testing.T, client *http.Client, url string, payload any) (int, []byte) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(b))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}