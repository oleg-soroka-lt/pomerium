package authenticate

import (
	"context"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/go-jose/go-jose/v3"

	"github.com/pomerium/pomerium/config"
	"github.com/pomerium/pomerium/internal/encoding"
	"github.com/pomerium/pomerium/internal/encoding/jws"
	"github.com/pomerium/pomerium/internal/sessions"
	"github.com/pomerium/pomerium/internal/sessions/cookie"
	"github.com/pomerium/pomerium/internal/urlutil"
	"github.com/pomerium/pomerium/pkg/cryptutil"
	"github.com/pomerium/pomerium/pkg/grpc"
	"github.com/pomerium/pomerium/pkg/grpc/databroker"
	"github.com/pomerium/pomerium/pkg/webauthnutil"
	"github.com/pomerium/webauthn"
)

var outboundGRPCConnection = new(grpc.CachedOutboundGRPClientConn)

type authenticateState struct {
	redirectURL *url.URL
	// sharedEncoder is the encoder to use to serialize data to be consumed
	// by other services
	sharedEncoder encoding.MarshalUnmarshaler
	// sharedKey is the secret to encrypt and authenticate data shared between services
	sharedKey []byte
	// sharedCipher is the cipher to use to encrypt/decrypt data shared between services
	sharedCipher cipher.AEAD
	// cookieSecret is the secret to encrypt and authenticate session data
	cookieSecret []byte
	// cookieCipher is the cipher to use to encrypt/decrypt session data
	cookieCipher cipher.AEAD
	// sessionStore is the session store used to persist a user's session
	sessionStore sessions.SessionStore
	// sessionLoaders are a collection of session loaders to attempt to pull
	// a user's session state from
	sessionLoader sessions.SessionLoader

	jwk *jose.JSONWebKeySet

	dataBrokerClient databroker.DataBrokerServiceClient

	webauthnRelyingParty *webauthn.RelyingParty
}

func newAuthenticateState() *authenticateState {
	return &authenticateState{
		jwk: new(jose.JSONWebKeySet),
	}
}

func newAuthenticateStateFromConfig(cfg *config.Config) (*authenticateState, error) {
	err := ValidateOptions(cfg.Options)
	if err != nil {
		return nil, err
	}

	state := &authenticateState{}

	authenticateURL, err := cfg.Options.GetAuthenticateURL()
	if err != nil {
		return nil, err
	}

	state.redirectURL, err = urlutil.DeepCopy(authenticateURL)
	if err != nil {
		return nil, err
	}

	state.redirectURL.Path = cfg.Options.AuthenticateCallbackPath

	// shared cipher to encrypt data before passing data between services
	state.sharedKey, err = cfg.Options.GetSharedKey()
	if err != nil {
		return nil, err
	}

	state.sharedCipher, err = cryptutil.NewAEADCipher(state.sharedKey)
	if err != nil {
		return nil, err
	}

	// shared state encoder setup
	state.sharedEncoder, err = jws.NewHS256Signer(state.sharedKey)
	if err != nil {
		return nil, err
	}

	// private state encoder setup, used to encrypt oauth2 tokens
	state.cookieSecret, err = cfg.Options.GetCookieSecret()
	if err != nil {
		return nil, err
	}

	state.cookieCipher, err = cryptutil.NewAEADCipher(state.cookieSecret)
	if err != nil {
		return nil, err
	}

	cookieStore, err := cookie.NewStore(func() cookie.Options {
		return cookie.Options{
			Name:     cfg.Options.CookieName,
			Domain:   cfg.Options.CookieDomain,
			Secure:   cfg.Options.CookieSecure,
			HTTPOnly: cfg.Options.CookieHTTPOnly,
			Expire:   cfg.Options.CookieExpire,
		}
	}, state.sharedEncoder)
	if err != nil {
		return nil, err
	}

	state.sessionStore = cookieStore
	state.sessionLoader = cookieStore
	state.jwk = new(jose.JSONWebKeySet)
	signingKey, err := cfg.Options.GetSigningKey()
	if err != nil {
		return nil, err
	}
	if signingKey != "" {
		decodedCert, err := base64.StdEncoding.DecodeString(cfg.Options.SigningKey)
		if err != nil {
			return nil, fmt.Errorf("authenticate: failed to decode signing key: %w", err)
		}
		jwk, err := cryptutil.PublicJWKFromBytes(decodedCert)
		if err != nil {
			return nil, fmt.Errorf("authenticate: failed to convert jwks: %w", err)
		}
		state.jwk.Keys = append(state.jwk.Keys, *jwk)
	}

	sharedKey, err := cfg.Options.GetSharedKey()
	if err != nil {
		return nil, err
	}

	dataBrokerConn, err := outboundGRPCConnection.Get(context.Background(), &grpc.OutboundOptions{
		OutboundPort:   cfg.OutboundPort,
		InstallationID: cfg.Options.InstallationID,
		ServiceName:    cfg.Options.Services,
		SignedJWTKey:   sharedKey,
	})
	if err != nil {
		return nil, err
	}

	state.dataBrokerClient = databroker.NewDataBrokerServiceClient(dataBrokerConn)

	state.webauthnRelyingParty = webauthn.NewRelyingParty(
		authenticateURL.String(),
		webauthnutil.NewCredentialStorage(state.dataBrokerClient),
	)

	return state, nil
}
