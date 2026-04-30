package jwt

import (
	"crypto"
	"errors"
	"fmt"

	gojwt "github.com/golang-jwt/jwt/v5"
	lua "github.com/akzj/go-lua/pkg/lua"
)

// Config configures the JWT module.
type Config struct {
	// Secret is the HMAC secret key (for HS256/HS384/HS512).
	// Mutually exclusive with PrivateKey/PublicKey.
	Secret []byte

	// Algorithm: "HS256" (default), "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512"
	Algorithm string

	// PrivateKey for RSA/ECDSA signing (optional, needed for jwt.sign with RSA/EC)
	PrivateKey crypto.PrivateKey

	// PublicKey for RSA/ECDSA verification (optional, needed for jwt.verify with RSA/EC)
	PublicKey crypto.PublicKey

	// Issuer, if set, is validated on verify and added on sign.
	Issuer string

	// Audience, if set, is validated on verify.
	Audience []string
}

// Register registers the "jwt" module in the Lua state.
func Register(L *lua.State, cfg Config) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = "HS256"
	}

	lua.RegisterModule(L, "jwt", map[string]lua.Function{
		"verify": lua.WrapSafe(jwtVerify(cfg)),
		"sign":   lua.WrapSafe(jwtSign(cfg)),
		"decode": lua.WrapSafe(jwtDecode(cfg)),
	})
}

// --- verify ---

func jwtVerify(cfg Config) lua.Function {
	return func(L *lua.State) int {
		tokenStr := L.CheckString(1)

		token, err := gojwt.Parse(tokenStr, func(token *gojwt.Token) (any, error) {
			return getVerifyKey(cfg, token)
		}, getParserOptions(cfg)...)

		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		if !token.Valid {
			L.PushNil()
			L.PushString("invalid token")
			return 2
		}

		claims, ok := token.Claims.(gojwt.MapClaims)
		if !ok {
			L.PushNil()
			L.PushString("failed to parse claims")
			return 2
		}

		L.PushAny(map[string]any(claims))
		return 1
	}
}

// --- sign ---

func jwtSign(cfg Config) lua.Function {
	return func(L *lua.State) int {
		if !L.IsTable(1) {
			L.PushNil()
			L.PushString("jwt.sign: argument must be a table")
			return 2
		}

		claims := gojwt.MapClaims{}
		L.ForEach(1, func(inner *lua.State) bool {
			k, _ := inner.ToString(-2)
			v := inner.ToAny(-1)
			claims[k] = v
			return true
		})

		// Add issuer if configured and not already set
		if cfg.Issuer != "" {
			if _, exists := claims["iss"]; !exists {
				claims["iss"] = cfg.Issuer
			}
		}

		method := getSigningMethod(cfg.Algorithm)
		token := gojwt.NewWithClaims(method, claims)

		key, err := getSignKey(cfg)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		tokenStr, err := token.SignedString(key)
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		L.PushString(tokenStr)
		return 1
	}
}

// --- decode (no verification) ---

func jwtDecode(_ Config) lua.Function {
	return func(L *lua.State) int {
		tokenStr := L.CheckString(1)

		parser := gojwt.NewParser(gojwt.WithoutClaimsValidation())
		token, _, err := parser.ParseUnverified(tokenStr, gojwt.MapClaims{})
		if err != nil {
			L.PushNil()
			L.PushString(err.Error())
			return 2
		}

		claims, ok := token.Claims.(gojwt.MapClaims)
		if !ok {
			L.PushNil()
			L.PushString("failed to parse claims")
			return 2
		}

		L.PushAny(map[string]any(claims))
		return 1
	}
}

// --- helpers ---

func getSigningMethod(alg string) gojwt.SigningMethod {
	switch alg {
	case "HS256":
		return gojwt.SigningMethodHS256
	case "HS384":
		return gojwt.SigningMethodHS384
	case "HS512":
		return gojwt.SigningMethodHS512
	case "RS256":
		return gojwt.SigningMethodRS256
	case "RS384":
		return gojwt.SigningMethodRS384
	case "RS512":
		return gojwt.SigningMethodRS512
	case "ES256":
		return gojwt.SigningMethodES256
	case "ES384":
		return gojwt.SigningMethodES384
	case "ES512":
		return gojwt.SigningMethodES512
	default:
		return gojwt.SigningMethodHS256
	}
}

func getVerifyKey(cfg Config, token *gojwt.Token) (any, error) {
	alg := cfg.Algorithm

	switch {
	case alg == "HS256" || alg == "HS384" || alg == "HS512":
		if _, ok := token.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		if len(cfg.Secret) == 0 {
			return nil, errors.New("no HMAC secret configured")
		}
		return cfg.Secret, nil

	case alg == "RS256" || alg == "RS384" || alg == "RS512":
		if _, ok := token.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		if cfg.PublicKey == nil {
			return nil, errors.New("no RSA public key configured")
		}
		return cfg.PublicKey, nil

	case alg == "ES256" || alg == "ES384" || alg == "ES512":
		if _, ok := token.Method.(*gojwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		if cfg.PublicKey == nil {
			return nil, errors.New("no ECDSA public key configured")
		}
		return cfg.PublicKey, nil
	}

	return nil, errors.New("unsupported algorithm: " + alg)
}

func getSignKey(cfg Config) (any, error) {
	switch {
	case cfg.Algorithm == "HS256" || cfg.Algorithm == "HS384" || cfg.Algorithm == "HS512":
		if len(cfg.Secret) == 0 {
			return nil, errors.New("no HMAC secret configured")
		}
		return cfg.Secret, nil
	default:
		if cfg.PrivateKey == nil {
			return nil, errors.New("no private key configured for " + cfg.Algorithm)
		}
		return cfg.PrivateKey, nil
	}
}

func getParserOptions(cfg Config) []gojwt.ParserOption {
	opts := []gojwt.ParserOption{
		gojwt.WithValidMethods([]string{cfg.Algorithm}),
	}
	if cfg.Issuer != "" {
		opts = append(opts, gojwt.WithIssuer(cfg.Issuer))
	}
	if len(cfg.Audience) > 0 {
		for _, aud := range cfg.Audience {
			opts = append(opts, gojwt.WithAudience(aud))
		}
	}
	return opts
}
