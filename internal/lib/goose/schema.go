package goose

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Goose struct tag parser ──────────────────────────────────
//
// Supported goose tags (comma-separated):
//
//   goose:"default:uuid"          → auto UUID string
//   goose:"default:random(11)"    → random string of length N
//   goose:"default:now"           → time.Now()
//   goose:"default:waiting"       → literal string value
//   goose:"required"              → (metadata, for validation)
//   goose:"unique"                → (metadata, for index creation)
//   goose:"index"                 → (metadata, for index creation)
//   goose:"ref:files"             → (metadata, for populate)
//
// Example:
//
//   type File struct {
//       ID     string  `bson:"_id" goose:"required,default:uuid"`
//       Slug   string  `bson:"slug" goose:"unique,default:random(11)"`
//       Status string  `bson:"status" goose:"default:waiting"`
//       Name   string  `bson:"name" goose:"required"`
//       OwnerID *string `bson:"ownerId,omitempty" goose:"ref:user,index"`
//       CreatedAt time.Time `bson:"createdAt" goose:"default:now"`
//       UpdatedAt time.Time `bson:"updatedAt" goose:"default:now"`
//   }

var randomLenRegex = regexp.MustCompile(`^random\((\d+)\)$`)

// applyDefaults scans struct fields for `goose` tags and applies default values.
// Only sets defaults on zero-value fields (empty string, zero time, etc).
func applyDefaults(doc interface{}) {
	v := reflect.ValueOf(doc)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		structField := t.Field(i)

		// Handle embedded structs (e.g. BaseModel with bson:",inline")
		if structField.Anonymous && field.Kind() == reflect.Struct {
			if field.CanAddr() {
				applyDefaults(field.Addr().Interface())
			}
			continue
		}

		tag := structField.Tag.Get("goose")
		if tag == "" {
			continue
		}

		parts := strings.Split(tag, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "default:") {
				defaultVal := part[len("default:"):]
				applyDefault(field, defaultVal)
			}
		}
	}
}

// applyDefault sets a default value on a field if it's zero-value.
func applyDefault(field reflect.Value, defaultVal string) {
	if !field.CanSet() {
		return
	}

	switch field.Kind() {
	case reflect.String:
		if field.String() != "" {
			return // already set
		}
		field.SetString(resolveStringDefault(defaultVal))

	case reflect.Struct:
		// time.Time
		if field.Type() == reflect.TypeOf(time.Time{}) {
			t := field.Interface().(time.Time)
			if !t.IsZero() {
				return // already set
			}
			if defaultVal == "now" {
				field.Set(reflect.ValueOf(time.Now()))
			}
		}

	case reflect.Bool:
		// zero bool = false
		if defaultVal == "true" {
			field.SetBool(true)
		}

	case reflect.Int, reflect.Int32, reflect.Int64:
		if field.Int() != 0 {
			return
		}
		if n, err := strconv.ParseInt(defaultVal, 10, 64); err == nil {
			field.SetInt(n)
		}

	case reflect.Float64:
		if field.Float() != 0 {
			return
		}
		if f, err := strconv.ParseFloat(defaultVal, 64); err == nil {
			field.SetFloat(f)
		}

	case reflect.Ptr:
		// Only set pointer defaults if explicitly requested and field is nil
		if !field.IsNil() {
			return
		}
		elemType := field.Type().Elem()
		if elemType.Kind() == reflect.String {
			s := resolveStringDefault(defaultVal)
			field.Set(reflect.ValueOf(&s))
		}
	}
}

// resolveStringDefault resolves a string default value.
func resolveStringDefault(defaultVal string) string {
	switch defaultVal {
	case "uuid":
		return uuid.New().String()
	default:
		// Check random(N) pattern
		if m := randomLenRegex.FindStringSubmatch(defaultVal); len(m) == 2 {
			n, _ := strconv.Atoi(m[1])
			return randomSlug(n)
		}
		// Literal string value
		return defaultVal
	}
}

// ── Schema metadata (for index/ref inspection) ──────────────

// FieldSchema holds parsed goose tag metadata for a single field.
type FieldSchema struct {
	BsonName string
	GoName   string
	Default  string
	Required bool
	Unique   bool
	Index    bool
	Ref      string // collection name for reference
}

// GetSchema returns parsed goose tag metadata for all fields.
func GetSchema[T any]() []FieldSchema {
	t := reflect.TypeOf((*T)(nil)).Elem()
	return parseSchema(t)
}

func parseSchema(t reflect.Type) []FieldSchema {
	var schemas []FieldSchema
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Recurse into embedded structs
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			schemas = append(schemas, parseSchema(sf.Type)...)
			continue
		}

		bsonTag := sf.Tag.Get("bson")
		bsonName := strings.Split(bsonTag, ",")[0]
		if bsonName == "" {
			bsonName = sf.Name
		}

		gooseTag := sf.Tag.Get("goose")
		if gooseTag == "" {
			schemas = append(schemas, FieldSchema{BsonName: bsonName, GoName: sf.Name})
			continue
		}

		fs := FieldSchema{BsonName: bsonName, GoName: sf.Name}
		for _, part := range strings.Split(gooseTag, ",") {
			part = strings.TrimSpace(part)
			switch {
			case strings.HasPrefix(part, "default:"):
				fs.Default = part[len("default:"):]
			case strings.HasPrefix(part, "ref:"):
				fs.Ref = part[len("ref:"):]
			case part == "required":
				fs.Required = true
			case part == "unique":
				fs.Unique = true
			case part == "index":
				fs.Index = true
			}
		}
		schemas = append(schemas, fs)
	}
	return schemas
}

// DescribeSchema prints a human-readable schema description (for debugging).
func DescribeSchema[T any]() string {
	schemas := GetSchema[T]()
	var sb strings.Builder
	for _, s := range schemas {
		sb.WriteString(fmt.Sprintf("  %s:", s.BsonName))
		if s.Default != "" {
			sb.WriteString(fmt.Sprintf(" default=%s", s.Default))
		}
		if s.Ref != "" {
			sb.WriteString(fmt.Sprintf(" ref=%s", s.Ref))
		}
		if s.Required {
			sb.WriteString(" required")
		}
		if s.Unique {
			sb.WriteString(" unique")
		}
		if s.Index {
			sb.WriteString(" index")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
