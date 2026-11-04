package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tikhonp/shelf/internal/source"
)

// Every text the bot sends is here, so they can be reviewed together. Plain
// English without decorative emoji, as docs/telegram.md asks.

const (
	textPrivate = "This bot is private."
	textHelp    = `Music Shelf downloads music into the library.

Send a link to an album, track, playlist, artist or label. I queue it and keep one message up to date while it downloads.

Send a few words, such as an album title, and I search albums on every source. Tap a result to download it.

/queue lists what is downloading and what waits.`

	textUnsupported = source.RefusedLinkText
	textQueueEmpty  = "Nothing is downloading or waiting."
	textDismissed   = "Nothing was queued."
	textNoSearch    = "No source in Shelf can search right now."
	textSearchDown  = "Search is not working right now. Try again in a minute."
	// textSeeLog stands in for an error that is shelf's own.
	textSeeLog = "Shelf's log says why"

	// Status lines.
	textQueued      = "Queued"
	textDownloading = "Downloading"
	textCanceled    = "Canceled"
	textFailed      = "Failed"
	textAlready     = "Already downloaded"

	// Buttons.
	buttonCancel   = "Cancel"
	buttonRetry    = "Retry"
	buttonMissing  = "Fetch missing"
	buttonOpen     = "Open in Shelf"
	buttonAll      = "Download all"
	buttonNotNow   = "Cancel"
	commandQueue   = "What is downloading and what waits"
	commandHelp    = "How to use the bot"
	answerCancel   = "Canceling"
	answerRetry    = "Queued again"
	answerQueued   = "Queued"
	answerEnded    = "This download has already ended."
	answerGone     = "This download is no longer in Shelf."
	answerNotRetry = "Only a failed, partial or canceled download can be retried."
	answerBroken   = "That did not work. Try again."
)

// textUnknownLink names the sources whose links the bot knows: "This link
// is not from Qobuz or Apple Music."
func textUnknownLink(names []string) string {
	return "This link is not from " + orList(names) + "."
}

func textNotSetUp(source string) string { return source + " is not set up in Shelf." }

func textQueueFailed(why string) string { return "Could not queue this download: " + why }

func textLookupFailed(source string) string {
	return "Could not read this link on " + source + ". Try again later."
}

func textNoSuch(source, kind string) string { return source + " has no such " + kind + "." }

// textConfirm asks before an artist or label download: "Download 34
// albums by Example Artist?"
func textConfirm(albums int, name string, label bool) string {
	by := "by"
	if label {
		by = "from"
	}
	return fmt.Sprintf("Download %s %s %s?", count(albums, "album"), by, name)
}

func textNoAlbums(name string) string { return name + " has no albums to download." }

func textSearchHeader(query string) string { return `Albums for "` + query + `":` }

func textNothingFound(query string) string { return `Nothing found for "` + query + `".` }

func textQueuedAhead(n int) string { return textQueued + ", " + strconv.Itoa(n) + " ahead" }

// textProgress is "Downloading, 3 of 12 tracks".
func textProgress(done, total int, noun string) string {
	return fmt.Sprintf("%s, %d of %s", textDownloading, done, count(total, noun))
}

// textDone is "Done: 12 tracks, FLAC 24-bit 96 kHz, 1.2 GB", with the parts
// that are known.
func textDone(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return "Done"
	}
	return "Done: " + strings.Join(kept, ", ")
}

func textNothingToDownload() string { return "Done: nothing to download" }

// textPartial is "Done with errors: 11 of 12 tracks".
func textPartial(done, total int, noun string) string {
	return fmt.Sprintf("Done with errors: %d of %s", done, count(total, noun))
}

func textFailedWith(why string) string {
	if why == "" {
		return textFailed
	}
	return textFailed + ": " + why
}

func textAlreadyOn(date string) string { return textAlready + " on " + date }

// count is "1 track" or "12 tracks".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// orList is "Qobuz", "Qobuz or Apple Music", or "Qobuz, Apple Music or
// Fake".
func orList(names []string) string {
	switch len(names) {
	case 0:
		return "a known source"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}
