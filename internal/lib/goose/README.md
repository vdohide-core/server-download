# goose — Mongoose-like ODM for Go + MongoDB

Lightweight, type-safe ODM library inspired by [Mongoose](https://mongoosejs.com/docs/guide.html).  
ใช้ **struct tags** ตั้งค่า schema per-field เหมือน Mongoose schema definition.

## Quick Start

```go
import "server-download/internal/lib/goose"

// 1. Connect
goose.Connect("mongodb://localhost:27017/mydb")
// or set from existing *mongo.Database
goose.SetDB(db)

// 2. Define model with goose tags
type File struct {
    ID        string    `bson:"_id" json:"id" goose:"required,default:uuid"`
    Name      string    `bson:"name" json:"name" goose:"required"`
    Slug      string    `bson:"slug" json:"slug" goose:"unique,default:random(11)"`
    Status    string    `bson:"status" json:"status" goose:"default:waiting"`
    OwnerID   *string   `bson:"ownerId,omitempty" json:"ownerId,omitempty" goose:"ref:user,index"`
    ParentID  *string   `bson:"parentId,omitempty" json:"parentId,omitempty" goose:"ref:files,index"`
    CreatedAt time.Time `bson:"createdAt" json:"createdAt" goose:"default:now"`
    UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt" goose:"default:now"`
}

// 3. Register model
var FileModel = goose.NewModel[File]("files")

// 4. Auto-create indexes (call once at startup)
FileModel.EnsureIndexes(ctx)

// 5. Use it
file := FileModel.New()   // auto: _id, slug, status, createdAt, updatedAt
file.Name = "hello.mp4"
FileModel.Create(ctx, file)
```

---

## Goose Struct Tags

ตั้งค่า schema per-field ด้วย `goose:"..."` tag (comma-separated):

| Tag | Mongoose equivalent | Description |
|-----|-------------------|-------------|
| `default:uuid` | `default: uuidv4` | Auto UUID v4 string |
| `default:random(N)` | `default: () => randomString(N)` | Random string ความยาว N |
| `default:now` | `timestamps: true` | `time.Now()` |
| `default:xxx` | `default: "xxx"` | Literal string value |
| `required` | `required: true` | Required field (metadata) |
| `unique` | `unique: true` | Unique index — auto-created by `EnsureIndexes()` |
| `index` | `index: true` | Index — auto-created by `EnsureIndexes()` |
| `ref:collection` | `ref: "Model"` | Reference collection (metadata) |

### Mongoose vs goose comparison

```ts
// Mongoose (TypeScript)
const fileSchema = new Schema({
    _id:      { type: String, required: true, default: uuidv4 },
    slug:     { type: String, unique: true, default: () => randomString(11) },
    status:   { type: String, enum: FileStatus, default: "waiting" },
    name:     { type: String, required: true },
    ownerId:  { type: String, ref: "User", index: true },
    parentId: { type: String, ref: "File", index: true },
}, { timestamps: true });
```

```go
// goose (Go)
type File struct {
    ID       string  `bson:"_id"      goose:"required,default:uuid"`
    Slug     string  `bson:"slug"     goose:"unique,default:random(11)"`
    Status   string  `bson:"status"   goose:"default:waiting"`
    Name     string  `bson:"name"     goose:"required"`
    OwnerID  *string `bson:"ownerId,omitempty"  goose:"ref:user,index"`
    ParentID *string `bson:"parentId,omitempty" goose:"ref:files,index"`
    CreatedAt time.Time `bson:"createdAt" goose:"default:now"`
    UpdatedAt time.Time `bson:"updatedAt" goose:"default:now"`
}
var FileModel = goose.NewModel[File]("files")
```

### Field types & omitempty

| Go type | bson tag | Behavior |
|---------|----------|----------|
| `string` | `bson:"field"` | เก็บเสมอ (รวม `""`) |
| `*string` | `bson:"field,omitempty"` | `nil` = ไม่เก็บใน DB |
| `*time.Time` | `bson:"field,omitempty"` | `nil` = ไม่เก็บใน DB |
| `*Struct` | `bson:"field,omitempty"` | `nil` = ไม่เก็บใน DB |

---

## API Reference

### Connection

```go
goose.Connect(uri string) error                   // Connect to MongoDB (parses DB name from URI)
goose.SetDB(db *mongo.Database)                    // Use existing connection
goose.DB() *mongo.Database                         // Get current database
goose.Collection(name string) *mongo.Collection    // Raw collection access
```

### Model Registration

```go
var FileModel = goose.NewModel[File]("files")
```

### Auto Index Creation

```go
// EnsureIndexes — reads goose tags and creates MongoDB indexes
// Creates unique index for `goose:"unique"` fields
// Creates regular index for `goose:"index"` fields
FileModel.EnsureIndexes(ctx)

// EnsureCompoundIndex — create compound index on multiple fields
FileModel.EnsureCompoundIndex(ctx, []string{"ownerId", "parentId"}, false)      // regular
FileModel.EnsureCompoundIndex(ctx, []string{"hash", "ownerId"}, true)           // unique
```

### Create / Insert

```go
// New() — create struct with defaults applied (from goose tags)
file := FileModel.New()
file.Name = "test.mp4"

// Create() — insert one document (auto-applies defaults)
result, err := FileModel.Create(ctx, file)

// Save() — alias for Create
result, err := FileModel.Save(ctx, file)

// InsertMany() — insert multiple documents
result, err := FileModel.InsertMany(ctx, []*File{file1, file2})
```

### Find / Query (Direct)

```go
// FindOne — find single document
file, err := FileModel.FindOne(ctx, bson.M{"name": "test"})

// FindByID — find by _id
file, err := FileModel.FindByID(ctx, "uuid-here")

// FindBySlug — find by slug
file, err := FileModel.FindBySlug(ctx, "abc123")

// Find — find multiple documents
files, err := FileModel.Find(ctx, bson.M{"type": "video"})

// FindRaw — find with custom options
cursor, err := FileModel.FindRaw(ctx, filter, opts)
```

### Query Builder (Chainable)

```go
// Mongoose:  Model.find(filter).sort({createdAt: -1}).limit(10).skip(20)
// goose:     Model.Query(filter).SortDesc("createdAt").Limit(10).Skip(20).Exec(ctx)

// Full example — find with sort, pagination, projection
results, err := FileModel.Query(bson.M{"status": "active"}).
    SortDesc("createdAt").                // sort by createdAt descending
    Limit(10).                            // max 10 results
    Skip(20).                             // skip first 20
    Select("name", "status", "slug").     // include only these fields
    Exec(ctx)                             // execute → []*File

// Single result
file, err := FileModel.Query(bson.M{"slug": "abc"}).
    Select("name", "status").
    One(ctx)                              // execute → *File

// Pagination helper (1-indexed page number)
results, err := FileModel.Query(bson.M{"type": "video"}).
    SortDesc("createdAt").
    Page(2, 20).                          // page 2, 20 per page → skip=20, limit=20
    Exec(ctx)

// Count matching documents
count, err := FileModel.Query(bson.M{"status": "active"}).Count(ctx)

// Exclude fields
results, err := FileModel.Query(bson.M{}).
    Exclude("password", "token").
    Exec(ctx)

// Compound sort
results, err := FileModel.Query(bson.M{}).
    Sort("status", 1).Sort("createdAt", -1).     // ASC status, DESC createdAt
    Exec(ctx)
```

#### Query Builder Methods

| Method | Description | Mongoose equivalent |
|--------|-------------|-------------------|
| `.Query(filter)` | Start query | `.find(filter)` |
| `.Sort(field, order)` | Sort field (1=asc, -1=desc) | `.sort({field: order})` |
| `.SortAsc(field)` | Shorthand sort ascending | `.sort({field: 1})` |
| `.SortDesc(field)` | Shorthand sort descending | `.sort({field: -1})` |
| `.Limit(n)` | Max results | `.limit(n)` |
| `.Skip(n)` | Skip results | `.skip(n)` |
| `.Page(page, size)` | Pagination helper | skip + limit combo |
| `.Select(fields...)` | Include fields | `.select("field1 field2")` |
| `.Exclude(fields...)` | Exclude fields | `.select("-field1 -field2")` |
| `.Exec(ctx)` | Execute → `[]*T` | await / .exec() |
| `.One(ctx)` | Execute → `*T` (first match) | `.findOne()` |
| `.Count(ctx)` | Count matches | `.countDocuments()` |

### Update

```go
// UpdateOne — update single document (auto-sets updatedAt)
result, err := FileModel.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"name": "new"}})

// UpdateByID — update by _id
result, err := FileModel.UpdateByID(ctx, "uuid", bson.M{"$set": bson.M{...}})

// UpdateMany — update multiple documents
result, err := FileModel.UpdateMany(ctx, filter, update)

// UpdateOneRaw — update without auto updatedAt
result, err := FileModel.UpdateOneRaw(ctx, filter, update)
```

### Delete

```go
// DeleteOne — delete single document
result, err := FileModel.DeleteOne(ctx, filter)

// DeleteByID — delete by _id
result, err := FileModel.DeleteByID(ctx, "uuid")

// DeleteMany — delete multiple documents
result, err := FileModel.DeleteMany(ctx, filter)
```

### Count / Aggregate

```go
// CountDocuments
count, err := FileModel.CountDocuments(ctx, filter)

// Exists — returns true if at least one match
exists, err := FileModel.Exists(ctx, filter)

// Aggregate — run aggregation pipeline
cursor, err := FileModel.Aggregate(ctx, pipeline)
```

### Raw Collection Access

```go
// Col() — get underlying *mongo.Collection
FileModel.Col().Indexes().CreateOne(ctx, indexModel)

// goose.Collection() — for collections without a model
goose.Collection("settings").FindOne(ctx, filter)
```

---

## File Structure

```
goose/
├── goose.go      # Connection: Connect(), SetDB(), DB(), Collection()
├── schema.go     # Struct tag parser: applyDefaults(), GetSchema()
├── base.go       # BaseModel (legacy, backward compat)
├── model.go      # Model[T]: NewModel(), New(), Col()
├── index.go      # EnsureIndexes(), EnsureCompoundIndex()
├── query.go      # Query builder: Query(), Sort(), Limit(), Exec()
├── insert.go     # Create(), Save(), InsertMany()
├── find.go       # FindOne(), FindByID(), FindBySlug(), Find()
├── update.go     # UpdateOne(), UpdateByID(), UpdateMany()
├── delete.go     # DeleteOne(), DeleteByID(), DeleteMany()
└── count.go      # CountDocuments(), Exists(), Aggregate()
```

---

## Schema Inspection (Debug)

```go
// Print schema metadata for a model
fmt.Println(goose.DescribeSchema[File]())
// Output:
//   _id: default=uuid required
//   slug: default=random(11) unique
//   status: default=waiting
//   name: required
//   ownerId: ref=user index
//   parentId: ref=files index
//   createdAt: default=now
//   updatedAt: default=now
```
