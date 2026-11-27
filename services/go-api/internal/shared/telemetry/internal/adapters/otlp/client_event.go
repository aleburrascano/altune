package otlp

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
)

const (
	clientEventBody  = "client_event"
	pseudoIDHexChars = 16
	sessionKey       = "session_id"
)

var allowedPayloadStrings = map[string]struct{}{
	"action": {}, "outcome": {}, "surface": {}, "source": {}, "source_kind": {},
	"kind": {}, "provider": {}, "status": {}, "entry_point": {}, "route": {},
	"reason": {}, "position": {},
}

type ClientEvent struct {
	Type          string
	EventID       string
	CorrelationID string
	AppVersion    string
	Platform      string
	UserID        string
	Payload       map[string]any
}

type ClientEvents struct {
	now    clock.Func
	key    []byte
	logger log.Logger
}

func NewClientEvents(now clock.Func, userKey string, logger log.Logger) *ClientEvents {
	if now == nil {
		panic("otlp.NewClientEvents: a nil clock")
	}
	return &ClientEvents{now: now, key: []byte(userKey), logger: logger}
}

func (c *ClientEvents) Emit(ctx context.Context, e ClientEvent) {
	var rec log.Record
	rec.SetTimestamp(c.now())
	rec.SetSeverity(log.SeverityInfo)
	rec.SetBody(attribute.StringValue(clientEventBody))
	rec.AddAttributes(c.attributes(e)...)
	c.logger.Emit(ctx, rec)
}

func (c *ClientEvents) attributes(e ClientEvent) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("event.type", e.Type),
		attribute.String("app.version", clip(e.AppVersion)),
		attribute.String("app.platform", clip(e.Platform)),
	}
	if e.EventID != "" {
		attrs = append(attrs, attribute.String("event.id", e.EventID))
	}
	if e.CorrelationID != "" {
		attrs = append(attrs, attribute.String("correlation.id", clip(e.CorrelationID)))
	}
	if len(c.key) > 0 {
		attrs = append(attrs, attribute.String("user.pseudo_id", c.pseudoID(e.UserID)))
	}
	return append(attrs, payloadAttributes(e.Payload)...)
}

func (c *ClientEvents) pseudoID(userID string) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(userID))
	return hex.EncodeToString(mac.Sum(nil))[:pseudoIDHexChars]
}

func payloadAttributes(payload map[string]any) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	for key, value := range payload {
		if kv, ok := payloadAttribute(key, value); ok {
			attrs = append(attrs, kv)
		}
	}
	return attrs
}

func sessionAttribute(value any) (attribute.KeyValue, bool) {
	if v, ok := value.(string); ok {
		return attribute.String("session.id", clip(v)), true
	}
	return attribute.KeyValue{}, false
}

func payloadAttribute(key string, value any) (attribute.KeyValue, bool) {
	name := "payload." + key
	if key == sessionKey {
		return sessionAttribute(value)
	}
	switch v := value.(type) {
	case bool:
		return attribute.Bool(name, v), true
	case float64:
		return attribute.Float64(name, v), true
	case string:
		if _, ok := allowedPayloadStrings[key]; ok {
			return attribute.String(name, clip(v)), true
		}
	}
	return attribute.KeyValue{}, false
}
