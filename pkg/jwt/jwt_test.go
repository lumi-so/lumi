package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func TestJWT_HMAC_SignAndVerify(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Secret:    []byte("test-secret-key-32bytes-long!!!!"),
		Algorithm: "HS256",
	})

	err := L.DoString(`
		local jwt = require("jwt")

		-- Sign
		local token, err = jwt.sign({
			sub = "user123",
			role = "admin",
			exp = os.time() + 3600,
		})
		assert(token ~= nil, "token should not be nil: " .. tostring(err))
		assert(err == nil)

		-- Verify
		local claims, err = jwt.verify(token)
		assert(claims ~= nil, "claims should not be nil: " .. tostring(err))
		assert(claims.sub == "user123", "sub should be user123")
		assert(claims.role == "admin", "role should be admin")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJWT_HMAC_InvalidToken(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Secret:    []byte("my-secret"),
		Algorithm: "HS256",
	})

	err := L.DoString(`
		local jwt = require("jwt")
		local claims, err = jwt.verify("invalid.token.here")
		assert(claims == nil, "claims should be nil for invalid token")
		assert(err ~= nil, "err should not be nil")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJWT_HMAC_ExpiredToken(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Secret:    []byte("my-secret"),
		Algorithm: "HS256",
	})

	err := L.DoString(`
		local jwt = require("jwt")

		-- Sign with expired time
		local token = jwt.sign({
			sub = "user123",
			exp = os.time() - 100,  -- already expired
		})
		assert(token ~= nil)

		-- Verify should fail
		local claims, err = jwt.verify(token)
		assert(claims == nil, "expired token should fail verification")
		assert(err ~= nil)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJWT_HMAC_WrongSecret(t *testing.T) {
	L1 := lua.NewState()
	defer L1.Close()
	Register(L1, Config{Secret: []byte("secret-1"), Algorithm: "HS256"})

	L2 := lua.NewState()
	defer L2.Close()
	Register(L2, Config{Secret: []byte("secret-2"), Algorithm: "HS256"})

	// Sign with L1
	err := L1.DoString(`
		local jwt = require("jwt")
		token = jwt.sign({sub = "user123", exp = os.time() + 3600})
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Get token string
	L1.GetGlobal("token")
	tokenStr, _ := L1.ToString(-1)
	L1.Pop(1)

	// Verify with L2 (different secret) — should fail
	L2.PushString(tokenStr)
	L2.SetGlobal("test_token")

	err = L2.DoString(`
		local jwt = require("jwt")
		local claims, err = jwt.verify(test_token)
		assert(claims == nil, "wrong secret should fail")
		assert(err ~= nil)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJWT_Decode_NoVerification(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Secret:    []byte("my-secret"),
		Algorithm: "HS256",
	})

	err := L.DoString(`
		local jwt = require("jwt")

		local token = jwt.sign({sub = "user123", role = "admin", exp = os.time() + 3600})

		-- Decode without verification
		local claims = jwt.decode(token)
		assert(claims ~= nil)
		assert(claims.sub == "user123")
		assert(claims.role == "admin")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJWT_Issuer_Validation(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Secret:    []byte("my-secret"),
		Algorithm: "HS256",
		Issuer:    "https://auth.example.com",
	})

	err := L.DoString(`
		local jwt = require("jwt")

		-- Sign (issuer auto-added)
		local token = jwt.sign({sub = "user123", exp = os.time() + 3600})

		-- Verify (issuer checked)
		local claims = jwt.verify(token)
		assert(claims ~= nil)
		assert(claims.iss == "https://auth.example.com")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestJWT_RSA_SignAndVerify(t *testing.T) {
	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Algorithm:  "RS256",
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	})

	luaErr := L.DoString(`
		local jwt = require("jwt")

		local token, err = jwt.sign({sub = "rsa-user", exp = os.time() + 3600})
		assert(token ~= nil, "RSA sign failed: " .. tostring(err))

		local claims, err = jwt.verify(token)
		assert(claims ~= nil, "RSA verify failed: " .. tostring(err))
		assert(claims.sub == "rsa-user")
	`)
	if luaErr != nil {
		t.Fatal(luaErr)
	}
}

func TestJWT_ECDSA_SignAndVerify(t *testing.T) {
	// Generate ECDSA key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	L := lua.NewState()
	defer L.Close()

	Register(L, Config{
		Algorithm:  "ES256",
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	})

	luaErr := L.DoString(`
		local jwt = require("jwt")

		local token, err = jwt.sign({sub = "ec-user", exp = os.time() + 3600})
		assert(token ~= nil, "ECDSA sign failed: " .. tostring(err))

		local claims, err = jwt.verify(token)
		assert(claims ~= nil, "ECDSA verify failed: " .. tostring(err))
		assert(claims.sub == "ec-user")
	`)
	if luaErr != nil {
		t.Fatal(luaErr)
	}
}
