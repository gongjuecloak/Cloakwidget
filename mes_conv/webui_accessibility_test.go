package main

import (
	"strings"
	"testing"
)

func TestEmbeddedUIAccessibilityContracts(t *testing.T) {
	html := string(embeddedIndex)
	for _, id := range []string{"helpModal", "aboutModal", "diffModal", "updateModal", "clModal", "authModal", "welcomeModal"} {
		start := strings.Index(html, `id="`+id+`"`)
		if start < 0 {
			t.Fatalf("missing modal %s", id)
		}
		end := strings.Index(html[start:], ">")
		if end < 0 {
			t.Fatalf("unterminated modal tag %s", id)
		}
		tag := html[start : start+end]
		if !strings.Contains(tag, `role="dialog"`) || !strings.Contains(tag, `aria-modal="true"`) {
			t.Fatalf("modal %s must expose dialog semantics", id)
		}
	}

	for _, id := range []string{"progressBar", "ordProgressBar", "bomProgressBar"} {
		start := strings.Index(html, `id="`+id+`"`)
		if start < 0 {
			t.Fatalf("missing progress bar %s", id)
		}
		end := strings.Index(html[start:], ">")
		tag := html[start : start+end]
		if !strings.Contains(tag, `role="progressbar"`) || !strings.Contains(tag, `aria-valuenow="0"`) {
			t.Fatalf("progress bar %s must expose its value", id)
		}
	}

	if !strings.Contains(string(embeddedApp), `setAttribute("aria-valuenow"`) {
		t.Fatal("progress updates must keep aria-valuenow in sync")
	}
}
