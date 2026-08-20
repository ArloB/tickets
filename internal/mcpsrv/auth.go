package mcpsrv

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// noExpirySentinel satisfies the MCP SDK's own bearer-token check,
// which unconditionally rejects any TokenInfo with a zero Expiration
// (see the SDK's auth.verify: "token missing expiration") — the SDK
// has no concept of an unlimited-lifetime token the way ADR 0004's
// optional agent-token expiry does. service.VerifyBearerToken has
// already checked the token's real expiry (or confirmed it has none)
// by the time tokenVerifier returns successfully, so this sentinel is
// never itself re-checked for anything; it exists purely to satisfy
// the SDK's non-zero requirement for a token our own service already
// vetted.
var noExpirySentinel = time.Now().AddDate(100, 0, 0)

// tokenVerifier adapts service.VerifyBearerToken to the MCP SDK's
// auth.TokenVerifier shape (ADR 0006). UserID carries the "kind:name"
// wire form (domain.ActorRef.String()) rather than a UUID: despite ADR
// 0004's original wording, kind:name is the actor identifier every
// other part of the system actually keys on (ADR 0012 — see also
// store.GetActorIDByRef), and resolving a UUID here would exist for no
// caller but this one.
func tokenVerifier(svc *service.Service) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		actor, err := svc.VerifyBearerToken(ctx, token)
		if err != nil {
			var svcErr *service.Error
			if errors.As(err, &svcErr) && svcErr.Code == domain.ErrUnauthorized {
				return nil, sdkauth.ErrInvalidToken
			}
			return nil, err
		}
		return &sdkauth.TokenInfo{UserID: actor.String(), Expiration: noExpirySentinel}, nil
	}
}
