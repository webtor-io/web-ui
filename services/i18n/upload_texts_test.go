package i18n

import (
	"regexp"
	"strings"
	"testing"
)

// The uploader's messages say what is happening and what to do, never how
// the storage is built: a full node is "storage is full right now", not "the
// storage node is full". The node is the seeder that holds the files, an
// inside detail.
func TestUploadTextsNameNoStorageNode(t *testing.T) {
	node := regexp.MustCompile(`(?i)\bnode\b|узл|узел|knoten|\bnodo\b|nœud|węz[eł]|\bnó\P{L}|düğüm|\buzl|\buzel|节点`)
	locales := localeFiles(t)
	if len(locales) != 12 {
		t.Fatalf("%d locales, want 12", len(locales))
	}
	for lang, msgs := range locales {
		for k, v := range msgs {
			if strings.HasPrefix(k, "upload.") && node.MatchString(v) {
				t.Errorf("%s: %s names the node: %q", lang, k, v)
			}
		}
	}
}

// ownFilesText: a text of the feature of one's own files.
func ownFilesText(k string) bool {
	for _, p := range []string{"upload.", "library.uploads", "vault.upload", "error.upload_", "embed.upload.", "tool.privateCloudMediaServer."} {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	switch k {
	case "library.uploadKept", "resource.upload", "private.page.uploadTitle", "profile.uploadsQuota",
		"profile.vault.ownFiles", "toast.uploadsDeleted":
		return true
	}
	return false
}

// One's own files go by one name, their library tab's, wherever the
// interface names them: the badge over the title on their page, the
// profile's line and Vault row, the own-files page's step 2, the toast after
// "Remove", the messages of a full space. The name says what is in it, as
// "My subtitles" does: "My uploads" reads as downloads in Russian, as
// recordings in Czech. A text that points to the tab quotes the name, so it
// is not inflected and is found as it is.
func TestOwnFilesGoByOneName(t *testing.T) {
	locales := localeFiles(t)
	if len(locales) != 12 {
		t.Fatalf("%d locales, want 12", len(locales))
	}
	for lang, want := range map[string]string{"en": "My files", "ru": "Мои файлы"} {
		if got := locales[lang]["resource.upload"]; got != want {
			t.Errorf("%s: the name is %q, want %q", lang, got, want)
		}
	}
	for lang, msgs := range locales {
		name := msgs["resource.upload"]
		if msgs["library.uploads"] != name {
			t.Errorf("%s: the tab is %q, the badge %q", lang, msgs["library.uploads"], name)
		}
		if msgs["profile.vault.ownFiles"] != name {
			t.Errorf("%s: the Vault row is %q, the badge %q", lang, msgs["profile.vault.ownFiles"], name)
		}
		for _, k := range []string{"profile.uploadsQuota", "library.uploadKept", "upload.quotaFull", "upload.quotaOut"} {
			if !strings.Contains(msgs[k], name) {
				t.Errorf("%s: %s does not say %q: %q", lang, k, name, msgs[k])
			}
		}
		const step2 = "tool.privateCloudMediaServer.about.steps.step2.text"
		if !strings.Contains(msgs[step2], "<strong>"+name+"</strong>") {
			t.Errorf("%s: step 2 does not point to <strong>%s</strong>: %q", lang, name, msgs[step2])
		}
	}
}

// Nor is an upload a thing the interface talks about in English or Russian:
// files are uploaded, and what is there is "files". The verb stays ("Upload
// a folder", "Uploading your files"); the noun goes ("Delete upload", "This
// upload is gone", "Временные загрузки").
func TestOwnFilesTextsSayFilesNotUploads(t *testing.T) {
	noun := map[string]*regexp.Regexp{
		"en": regexp.MustCompile(`(?i)\buploads\b|\b(?:a|an|the|this|that|new|one|each|every|your|my|private|temporary|delete) upload\b`),
		"ru": regexp.MustCompile(`(?i)загрузк`),
	}
	locales := localeFiles(t)
	for lang, re := range noun {
		n := 0
		for k, v := range locales[lang] {
			if !ownFilesText(k) {
				continue
			}
			n++
			if m := re.FindString(v); m != "" {
				t.Errorf("%s: %s says %q: %q", lang, k, m, v)
			}
		}
		if n < 50 {
			t.Errorf("%s: %d texts of one's own files, the prefixes miss some", lang, n)
		}
	}
}

// No message of one's own files asks to create anything: there is no button
// that creates an upload, a drop does.
func TestOwnFilesTextsAskToCreateNothing(t *testing.T) {
	create := regexp.MustCompile(`(?i)\bcreate|созда|erstell|\bcrea(?:ne)?\b|\bcréez|\bmaak\b|\bcrie\b|oluştur|vytvoř|创建`)
	locales := localeFiles(t)
	if len(locales) != 12 {
		t.Fatalf("%d locales, want 12", len(locales))
	}
	for lang, msgs := range locales {
		for k, v := range msgs {
			if ownFilesText(k) && create.MatchString(v) {
				t.Errorf("%s: %s asks to create: %q", lang, k, v)
			}
		}
	}
}
