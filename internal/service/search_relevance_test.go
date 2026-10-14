package service

import (
	"reflect"
	"testing"
)

func resetSearchRelevanceEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"SEARCH_TITLE_WEIGHT",
		"SEARCH_DESCRIPTION_WEIGHT",
		"SEARCH_RECENCY_WEIGHT",
		"SEARCH_SYNONYMS_JSON",
	} {
		t.Setenv(name, "")
	}
}

func TestSearchRelevanceOptionsDefaults(t *testing.T) {
	resetSearchRelevanceEnv(t)

	options, err := NewSearchRelevanceOptionsFromEnv()
	if err != nil {
		t.Fatalf("NewSearchRelevanceOptionsFromEnv() error = %v", err)
	}
	if !options.Configured || options.TitleWeight != 1 || options.DescriptionWeight != 0.4 || options.RecencyWeight != 0 {
		t.Fatalf("unexpected defaults: %+v", options)
	}
	if len(options.Synonyms) != 0 {
		t.Fatalf("default synonyms = %#v, want empty", options.Synonyms)
	}
}

func TestSearchRelevanceOptionsNormalizesSymmetricSynonyms(t *testing.T) {
	resetSearchRelevanceEnv(t)
	t.Setenv("SEARCH_TITLE_WEIGHT", "2")
	t.Setenv("SEARCH_DESCRIPTION_WEIGHT", "0.25")
	t.Setenv("SEARCH_RECENCY_WEIGHT", "0.75")
	t.Setenv("SEARCH_SYNONYMS_JSON", `{" Bug ":[" defect ","issue","defect"],"Sign   In":["login"]}`)

	options, err := NewSearchRelevanceOptionsFromEnv()
	if err != nil {
		t.Fatalf("NewSearchRelevanceOptionsFromEnv() error = %v", err)
	}
	if options.TitleWeight != 2 || options.DescriptionWeight != 0.25 || options.RecencyWeight != 0.75 {
		t.Fatalf("unexpected weights: %+v", options)
	}
	if !reflect.DeepEqual(options.Synonyms["bug"], []string{"defect", "issue"}) {
		t.Fatalf("bug synonyms = %#v", options.Synonyms["bug"])
	}
	if !reflect.DeepEqual(options.Synonyms["defect"], []string{"bug", "issue"}) {
		t.Fatalf("defect synonyms = %#v", options.Synonyms["defect"])
	}
	if !reflect.DeepEqual(options.Synonyms["sign in"], []string{"login"}) ||
		!reflect.DeepEqual(options.Synonyms["login"], []string{"sign in"}) {
		t.Fatalf("multi-word synonyms = %#v", options.Synonyms)
	}
}

func TestSearchRelevanceOptionsRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "negative title weight", key: "SEARCH_TITLE_WEIGHT", value: "-1"},
		{name: "oversized recency weight", key: "SEARCH_RECENCY_WEIGHT", value: "11"},
		{name: "invalid synonym json", key: "SEARCH_SYNONYMS_JSON", value: "not-json"},
		{name: "empty synonym term", key: "SEARCH_SYNONYMS_JSON", value: `{"bug":[""]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetSearchRelevanceEnv(t)
			t.Setenv(tt.key, tt.value)
			if _, err := NewSearchRelevanceOptionsFromEnv(); err == nil {
				t.Fatal("NewSearchRelevanceOptionsFromEnv() error = nil, want validation error")
			}
		})
	}
}

func TestExpandSearchSynonymsUsesPhraseAndTokens(t *testing.T) {
	synonyms := map[string][]string{
		"login": {"sign in"},
		"bug":   {"defect", "issue"},
	}
	got := expandSearchSynonyms("login bug", synonyms)
	want := []string{"sign in", "defect", "issue"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandSearchSynonyms() = %#v, want %#v", got, want)
	}
}
