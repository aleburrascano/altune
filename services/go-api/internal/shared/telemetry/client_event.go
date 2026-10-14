package telemetry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"

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
	Type       string
	EventID    string
	AppVersion string
	Platform   string
	UserID     string
	Payload    map[string]any
}

type ClientEvents struct {
	key    []byte
	logger log.Logger
}

func NewClientEvents(userKey string, logger log.Logger) *ClientEvents {
	return &ClientEvents{key: []byte(userKey), logger: logger}
}

func (c *ClientEvents) Emit(ctx context.Context, e ClientEvent) {
	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(log.SeverityInfo)
	rec.SetBody(attribute.StringValue(clientEventBody))
	rec.AddAttributes(c.attributes(e)...)
	c.logger.Emit(ctx, rec)
}

func (c *ClientEvents) attributes(e ClientEvent) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("event.type", e.Type),
		attribute.String("app.version", Clip(e.AppVersion)),
		attribute.String("app.platform", Clip(e.Platform)),
	}
	if e.EventID != "" {
		attrs = append(attrs, attribute.String("event.id", e.EventID))
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
		return attribute.String("session.id", Clip(v)), true
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
			return attribute.String(name, Clip(v)), true
		}
	}
	return attribute.KeyValue{}, false
}
