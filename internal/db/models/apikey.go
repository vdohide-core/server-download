package models

import (
	"time"

	"server-download/internal/lib/goose"
)

// ApiKey represents an API key for programmatic access.
// Collection: "apikeys" | _id: String (UUID)
type ApiKey struct {
	ID          string     `bson:"_id" json:"id" goose:"required,default:uuid"`
	Name        string     `bson:"name" json:"name" goose:"required"`
	Key         string     `bson:"key" json:"-"`
	Prefix      string     `bson:"prefix" json:"prefix"`
	OwnerID     string     `bson:"ownerId" json:"ownerId" goose:"ref:user,index"`
	LastUsedAt  *time.Time `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	ExpiresAt   *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
	Enabled     bool       `bson:"enabled" json:"enabled"`
	Permissions []string   `bson:"permissions" json:"permissions"` // read, upload, delete, manage
	CreatedAt   time.Time  `bson:"createdAt" json:"createdAt" goose:"default:now"`
	UpdatedAt   time.Time  `bson:"updatedAt" json:"updatedAt" goose:"default:now"`
}

// ApiKeyModel is the goose model for the "apikeys" collection.
var ApiKeyModel = goose.NewModel[ApiKey]("apikeys")
