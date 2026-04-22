package models

import (
	"time"

	"server-download/internal/lib/goose"
)

// WorkspaceMember represents a workspace membership.
// Collection: "workspace_members" | _id: String (UUID)
type WorkspaceMember struct {
	ID        string    `bson:"_id" json:"id" goose:"required,default:uuid"`
	SpaceID   string    `bson:"spaceId" json:"spaceId" goose:"ref:files,index"`
	UserID    string    `bson:"userId" json:"userId" goose:"ref:user,index"`
	Role      string    `bson:"role" json:"role"` // OWNER, ADMIN, MEMBER, VIEWER
	InvitedBy *string   `bson:"invitedBy,omitempty" json:"invitedBy,omitempty" goose:"ref:user"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt" goose:"default:now"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt" goose:"default:now"`
}

// WorkspaceMemberModel is the goose model for the "workspace_members" collection.
var WorkspaceMemberModel = goose.NewModel[WorkspaceMember]("workspace_members")
