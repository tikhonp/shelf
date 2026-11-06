// Package identity carries authors without coupling modules to account storage.
package identity

import (
	"context"
	"strconv"
)

type key struct{}

// Author is the identity and name captured when someone requests an action.
type Author struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func Account(id int64, username string) Author {
	return Author{"user:" + strconv.FormatInt(id, 10), username}
}
func Telegram(id int64, name string) Author {
	if name == "" {
		name = "Telegram " + strconv.FormatInt(id, 10)
	}
	return Author{"telegram:" + strconv.FormatInt(id, 10), name}
}
func With(ctx context.Context, a Author) context.Context { return context.WithValue(ctx, key{}, a) }
func From(ctx context.Context) Author                    { a, _ := ctx.Value(key{}).(Author); return a }
func Job(ctx context.Context, fallback string) string {
	if a := From(ctx); a.ID != "" && a.ID != "anonymous" {
		return a.ID
	}
	return fallback
}
func (a Author) Label() string {
	if a.Name == "" {
		return "Unknown"
	}
	return a.Name
}
