package models

import (
	"time"

	"server-download/internal/lib/goose"
)

// PlayerConfig holds video player configuration for a custom domain.
type PlayerConfig struct {
	LogoImageURL    *string `bson:"logoImageUrl,omitempty" json:"logoImageUrl,omitempty"`
	LogoWebsiteURL  *string `bson:"logoWebsiteUrl,omitempty" json:"logoWebsiteUrl,omitempty"`
	LogoPosition    *string `bson:"logoPosition,omitempty" json:"logoPosition,omitempty"`
	PosterURL       *string `bson:"posterUrl,omitempty" json:"posterUrl,omitempty"`
	BaseColor       string  `bson:"baseColor" json:"baseColor"`
	DisplayTitle    bool    `bson:"displayTitle" json:"displayTitle"`
	AutoPlay        bool    `bson:"autoPlay" json:"autoPlay"`
	MuteSound       bool    `bson:"muteSound" json:"muteSound"`
	RepeatVideo     bool    `bson:"repeatVideo" json:"repeatVideo"`
	ContinuePlay    bool    `bson:"continuePlay" json:"continuePlay"`
	ContinuePlayArk bool    `bson:"continuePlayArk" json:"continuePlayArk"`
	Sharing         bool    `bson:"sharing" json:"sharing"`
	Captions        bool    `bson:"captions" json:"captions"`
	PlaybackRate    bool    `bson:"playbackRate" json:"playbackRate"`
	Keyboard        bool    `bson:"keyboard" json:"keyboard"`
	Download        bool    `bson:"download" json:"download"`
	PIP             bool    `bson:"pip" json:"pip"`
	ShowPreviewTime bool    `bson:"showPreviewTime" json:"showPreviewTime"`
	FastForward     bool    `bson:"fastForward" json:"fastForward"`
	Rewind          bool    `bson:"rewind" json:"rewind"`
	SeekStep        int     `bson:"seekStep" json:"seekStep"`
}

// Advert holds a video advertisement config.
type Advert struct {
	ID          *string `bson:"id,omitempty" json:"id,omitempty"`
	Name        *string `bson:"name,omitempty" json:"name,omitempty"`
	MP4URL      *string `bson:"mp4Url,omitempty" json:"mp4Url,omitempty"`
	WebsiteURL  *string `bson:"websiteUrl,omitempty" json:"websiteUrl,omitempty"`
	SkipSeconds int     `bson:"skipSeconds" json:"skipSeconds"`
	IsActive    *bool   `bson:"isActive,omitempty" json:"isActive,omitempty"`
}

// AdvertImage holds an image advertisement config.
type AdvertImage struct {
	ID         *string  `bson:"id,omitempty" json:"id,omitempty"`
	Name       *string  `bson:"name,omitempty" json:"name,omitempty"`
	ImageURL   *string  `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"`
	WebsiteURL *string  `bson:"websiteUrl,omitempty" json:"websiteUrl,omitempty"`
	IsActive   *bool    `bson:"isActive,omitempty" json:"isActive,omitempty"`
	ShowOn     []string `bson:"showOn,omitempty" json:"showOn,omitempty"` // ready, end, pause
}

// DomainDNS holds DNS configuration for domain verification.
type DomainDNS struct {
	RecordType        string     `bson:"recordType" json:"recordType"`
	Value             string     `bson:"value" json:"value"`
	TTL               int        `bson:"ttl" json:"ttl"`
	VerificationToken string     `bson:"verificationToken" json:"verificationToken"`
	LastVerified      *time.Time `bson:"lastVerified,omitempty" json:"lastVerified,omitempty"`
}

// CustomDomain represents a custom domain with player/ad config.
// Collection: "custom_domains" | _id: String (UUID)
type CustomDomain struct {
	ID               string        `bson:"_id" json:"id" goose:"required,default:uuid"`
	Enable           bool          `bson:"enable" json:"enable"`
	Name             string        `bson:"name" json:"name" goose:"required,unique"`
	Status           string        `bson:"status" json:"status" goose:"default:pending"` // pending, active, failed, expired
	OwnerID          *string       `bson:"ownerId,omitempty" json:"ownerId,omitempty" goose:"ref:user,index"`
	DNS              *DomainDNS    `bson:"dns,omitempty" json:"dns,omitempty"`
	Player           *PlayerConfig `bson:"player,omitempty" json:"player,omitempty"`
	Advert           []Advert      `bson:"advert,omitempty" json:"advert,omitempty"`
	AdvertImage      *AdvertImage  `bson:"advertImage,omitempty" json:"advertImage,omitempty"`
	AdvertJavascript *string       `bson:"advertJavascript,omitempty" json:"advertJavascript,omitempty"`
	CreatedAt        time.Time     `bson:"createdAt" json:"createdAt" goose:"default:now"`
	UpdatedAt        time.Time     `bson:"updatedAt" json:"updatedAt" goose:"default:now"`
}

// CustomDomainModel is the goose model for the "custom_domains" collection.
var CustomDomainModel = goose.NewModel[CustomDomain]("custom_domains")
