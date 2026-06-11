package datasource

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

// IngestPost writes one Post to the per-network log and publishes the
// resulting MessageEvent plus optional preview enqueue. The caller resolves
// bufferID once (typically via MultiStore.EnsureBuffer at Start) and reuses
// it across polls, so each post costs one INSERT instead of a buffer-ensure
// round-trip. Returns (id, inserted, err); inserted=false means the
// (buffer_id, msgid) dedupe rejected the post.
func IngestPost(ctx context.Context, deps Deps, networkID, bufferID uuid.UUID, post Post) (uuid.UUID, bool, error) {
	if bufferID == uuid.Nil {
		return uuid.Nil, false, fmt.Errorf("datasource: empty bufferID")
	}
	logStore, err := deps.Stores.LogStore(networkID)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("log store: %w", err)
	}

	id, storedTS, inserted, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{
		BufferID:  bufferID,
		MsgID:     post.MsgID,
		Timestamp: post.Timestamp,
		Sender:    post.Sender,
		Account:   post.Account,
		Kind:      post.Kind,
		Target:    post.Target,
		Content:   post.Content,
	})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("insert message: %w", err)
	}
	if !inserted {
		return id, false, nil
	}

	if deps.Hub != nil {
		// Empty nick: datasource has no "self" concept, so semantic flags
		// (IsSelf, MentionsMe) stay zero. WithSemantics still computes the
		// canonical DisplayKind from kind/content (e.g. action vs. privmsg).
		deps.Hub.Publish((&irc.MessageEvent{
			Type: "message",
			MessageCore: irc.MessageCore{
				ID:        id,
				NetworkID: networkID,
				BufferID:  bufferID,
				MsgID:     post.MsgID,
				TS:        storedTS,
				Sender:    post.Sender,
				Account:   post.Account,
				Kind:      post.Kind,
				Target:    post.Target,
				Content:   post.Content,
			},
		}).WithSemantics(""))
	}

	if deps.Previews != nil && post.Content != "" {
		switch post.Kind {
		case "privmsg", "notice", "action":
			deps.Previews.Enqueue(networkID, bufferID, id, post.Content)
		}
	}

	return id, true, nil
}
