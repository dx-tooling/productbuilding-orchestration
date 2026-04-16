package domain

import (
	"testing"
)

func TestTranslations_AllEnglishKeysHaveGermanTranslation(t *testing.T) {
	for key := range translations["en"] {
		if _, ok := translations["de"][key]; !ok {
			t.Errorf("missing German translation for key %q", key)
		}
	}
}

func TestTranslations_AllGermanKeysHaveEnglishTranslation(t *testing.T) {
	for key := range translations["de"] {
		if _, ok := translations["en"][key]; !ok {
			t.Errorf("German key %q has no English counterpart", key)
		}
	}
}

func TestLocalizedMsg_ReturnsCorrectLanguage(t *testing.T) {
	en := LocalizedMsg("en", msgPreviewLive)
	de := LocalizedMsg("de", msgPreviewLive)
	if en == de {
		t.Error("English and German messages should differ")
	}
	if en == "" || de == "" {
		t.Error("neither translation should be empty")
	}
}

func TestLocalizedMsg_UnknownLanguageFallsToEnglish(t *testing.T) {
	msg := LocalizedMsg("fr", msgPreviewLive)
	en := LocalizedMsg("en", msgPreviewLive)
	if msg != en {
		t.Errorf("unknown language should fall back to English, got %q", msg)
	}
}
