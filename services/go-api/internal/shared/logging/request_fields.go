package logging

import (
	"context"
	"log/slog"
)

type RequestFields struct {
	UserID string
	Route  string
}

type requestFieldsKey struct{}

func ContextWithRequestFields(ctx context.Context, resolve func() RequestFields) context.Context {
	return context.WithValue(ctx, requestFieldsKey{}, resolve)
}

func ContextWithSubjectUser(ctx context.Context, userID string) context.Context {
	previous, _ := ctx.Value(requestFieldsKey{}).(func() RequestFields)
	return ContextWithRequestFields(ctx, func() RequestFields {
		var fields RequestFields
		if previous != nil {
			fields = previous()
		}
		if userID != "" {
			fields.UserID = userID
		}
		return fields
	})
}

func requestFieldStamps(ctx context.Context, r slog.Record) []slog.Attr {
	resolve, ok := ctx.Value(requestFieldsKey{}).(func() RequestFields)
	if !ok {
		return nil
	}
	fields := resolve()
	var stamps []slog.Attr
	if fields.UserID != "" && !recordHasKey(r, KeyUserID) {
		stamps = append(stamps, slog.String(KeyUserID, fields.UserID))
	}
	if fields.Route != "" && !recordHasKey(r, KeyRoute) {
		stamps = append(stamps, slog.String(KeyRoute, fields.Route))
	}
	return stamps
}
