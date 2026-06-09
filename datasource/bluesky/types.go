package bluesky

// CreateSessionRequest is the body of com.atproto.server.createSession.
type CreateSessionRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// SessionResponse is the response from createSession and refreshSession.
// Only fields Lurker consumes are mapped.
type SessionResponse struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	Handle     string `json:"handle"`
	DID        string `json:"did"`
}

// FeedResponse is the response from app.bsky.feed.getTimeline.
type FeedResponse struct {
	Feed   []FeedItem `json:"feed"`
	Cursor string     `json:"cursor"`
}

// FeedItem is one entry in a timeline page.
type FeedItem struct {
	Post   PostView      `json:"post"`
	Reason *FeedReason   `json:"reason,omitempty"`
	Reply  *FeedReplyRef `json:"reply,omitempty"`
}

// FeedReplyRef is the feed-level reply context. Unlike the record-level
// ReplyRef (which carries only uri/cid strong refs), the timeline embeds the
// full parent/root PostViews when they are visible, letting us render a quote
// snippet without a separate fetch. A parent that is not found, blocked, or
// otherwise unhydrated decodes into a PostView with only URI populated.
type FeedReplyRef struct {
	Root   *PostView `json:"root,omitempty"`
	Parent *PostView `json:"parent,omitempty"`
}

// PostView is the public view of a single post.
type PostView struct {
	URI       string `json:"uri"`
	CID       string `json:"cid"`
	Author    Actor  `json:"author"`
	Record    Record `json:"record"`
	Embed     *Embed `json:"embed,omitempty"`
	IndexedAt string `json:"indexedAt"`
}

// Actor is the author of a post.
type Actor struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
}

// Record is the on-wire post record. ATProto delivers extra fields we
// ignore (langs, facets, labels, …).
type Record struct {
	Type      string    `json:"$type,omitempty"`
	Text      string    `json:"text"`
	CreatedAt string    `json:"createdAt,omitempty"`
	Reply     *ReplyRef `json:"reply,omitempty"`
}

// ReplyRef points at the parent/root of a reply.
type ReplyRef struct {
	Parent *StrongRef `json:"parent,omitempty"`
	Root   *StrongRef `json:"root,omitempty"`
}

// StrongRef is the (uri, cid) tuple ATProto uses to reference a record.
type StrongRef struct {
	URI string `json:"uri"`
	CID string `json:"cid,omitempty"`
}

// FeedReason marks a feed item as a repost or pin. Discriminated by $type.
type FeedReason struct {
	Type string `json:"$type"`
	By   Actor  `json:"by"`
}

// Embed is the union of embedded content kinds we care about. All fields
// optional; presence depends on $type. Video views (app.bsky.embed.video#view)
// carry playlist/thumbnail/alt at the top level rather than under a nested key.
type Embed struct {
	Type      string         `json:"$type,omitempty"`
	External  *EmbedExternal `json:"external,omitempty"`
	Images    []EmbedImage   `json:"images,omitempty"`
	Playlist  string         `json:"playlist,omitempty"`  // video#view: m3u8
	Thumbnail string         `json:"thumbnail,omitempty"` // video#view: poster image
	Alt       string         `json:"alt,omitempty"`       // video#view alt text
	Media     *Embed         `json:"media,omitempty"`     // recordWithMedia wraps a nested embed
	Record    *EmbedRecord   `json:"record,omitempty"`
}

// EmbedExternal is an OG card.
type EmbedExternal struct {
	URI         string `json:"uri"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// EmbedImage is one image in an image embed.
type EmbedImage struct {
	Alt      string         `json:"alt,omitempty"`
	Fullsize string         `json:"fullsize,omitempty"`
	Thumb    string         `json:"thumb,omitempty"`
	Image    *EmbedImageRef `json:"image,omitempty"`
}

// EmbedImageRef carries the underlying blob ref when image URLs are absent.
type EmbedImageRef struct {
	MimeType string `json:"mimeType,omitempty"`
}

// EmbedRecord models the `record` field of a quote embed. For a plain quote
// (app.bsky.embed.record#view) it is the quoted post directly: Author and
// Value (the app.bsky.feed.post record) are populated. For a
// record-with-media embed the quoted post sits one level deeper under Record.
// Non-post union members (viewNotFound, viewBlocked, viewDetached, feed
// generators, lists, starter packs) leave Author/Value empty and are skipped.
type EmbedRecord struct {
	Type   string       `json:"$type,omitempty"`
	URI    string       `json:"uri,omitempty"`
	Author Actor        `json:"author,omitempty"`
	Value  *Record      `json:"value,omitempty"`
	Record *EmbedRecord `json:"record,omitempty"`
}
