package internal

import (
	"testing"

	"github.com/google/uuid"
)

func TestHashPassword(t *testing.T) {
	password := "password"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Error hashing password: %v", err)
	}
	if hashedPassword == "unset" {
		t.Fatalf("Hashed password is unset")
	}
}

func TestCheckPasswordHash(t *testing.T) {
	password := "password"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Error hashing password: %v", err)
	}
	isGood, err := CheckPasswordHash(password, hashedPassword)
	if err != nil {
		t.Fatalf("Error checking password hash: %v", err)
	}
	if !isGood {
		t.Fatalf("Password hash is not correct")
	}
}

func TestWrongPassword(t *testing.T) {
	password := "password"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Error hashing password: %v", err)
	}
	isGood, err := CheckPasswordHash("wrongpassword", hashedPassword)
	if err != nil {
		t.Fatalf("Error checking password hash: %v", err)
	}
	if isGood {
		t.Fatalf("Password hash is correct")
	}
}

func TestMakeJWT(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "tokenSecret"
	token, err := MakeJWT(userID, tokenSecret)
	if err != nil {
		t.Fatalf("Error making JWT: %v", err)
	}
	if token == "" {
		t.Fatalf("Token is empty")
	}
}

func TestValidateJWT(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "tokenSecret"
	token, err := MakeJWT(userID, tokenSecret)
	if err != nil {
		t.Fatalf("Error making JWT: %v", err)
	}
	validUserID, err := ValidateJWT(token, tokenSecret)
	if err != nil {
		t.Fatalf("Error validating JWT: %v", err)
	}
	if validUserID != userID {
		t.Fatalf("Valid user ID is not correct")
	}
}

func TestExpiredJWT(t *testing.T) {
	userID := uuid.New()
	tokenSecret := "tokenSecret"
	_, err := MakeJWT(userID, tokenSecret)
	if err != nil && err.Error() != "token expired" {
		t.Fatalf("Error making JWT: %v", err)
	}
}
