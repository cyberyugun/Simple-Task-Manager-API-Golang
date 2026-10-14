package service

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"go-simple-task-api/internal/model"
)

const (
	maxSearchSynonymGroups       = 128
	maxSearchSynonymsPerGroup    = 16
	maxSearchSynonymTermLength   = 80
	maxSearchExpandedSynonymTerm = 64
)

func NewSearchRelevanceOptionsFromEnv() (model.SearchRelevanceOptions, error) {
	options := model.DefaultSearchRelevanceOptions()

	var err error
	if options.TitleWeight, err = searchWeightFromEnv("SEARCH_TITLE_WEIGHT", options.TitleWeight); err != nil {
		return model.SearchRelevanceOptions{}, err
	}
	if options.DescriptionWeight, err = searchWeightFromEnv("SEARCH_DESCRIPTION_WEIGHT", options.DescriptionWeight); err != nil {
		return model.SearchRelevanceOptions{}, err
	}
	if options.RecencyWeight, err = searchWeightFromEnv("SEARCH_RECENCY_WEIGHT", options.RecencyWeight); err != nil {
		return model.SearchRelevanceOptions{}, err
	}

	raw := strings.TrimSpace(os.Getenv("SEARCH_SYNONYMS_JSON"))
	if raw == "" || raw == "{}" {
		return options, nil
	}

	var groups map[string][]string
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		return model.SearchRelevanceOptions{}, fmt.Errorf("SEARCH_SYNONYMS_JSON must be a JSON object of string arrays: %w", err)
	}
	if len(groups) > maxSearchSynonymGroups {
		return model.SearchRelevanceOptions{}, fmt.Errorf("SEARCH_SYNONYMS_JSON must contain at most %d groups", maxSearchSynonymGroups)
	}

	normalized := make(map[string][]string)
	for rawKey, rawAliases := range groups {
		if len(rawAliases) > maxSearchSynonymsPerGroup {
			return model.SearchRelevanceOptions{}, fmt.Errorf("SEARCH_SYNONYMS_JSON group %q must contain at most %d aliases", rawKey, maxSearchSynonymsPerGroup)
		}
		key, err := validateSearchSynonymTerm(rawKey)
		if err != nil {
			return model.SearchRelevanceOptions{}, err
		}

		terms := []string{key}
		seen := map[string]struct{}{key: {}}
		for _, rawAlias := range rawAliases {
			alias, err := validateSearchSynonymTerm(rawAlias)
			if err != nil {
				return model.SearchRelevanceOptions{}, err
			}
			if _, exists := seen[alias]; exists {
				continue
			}
			seen[alias] = struct{}{}
			terms = append(terms, alias)
		}

		for _, term := range terms {
			for _, other := range terms {
				if term == other {
					continue
				}
				normalized[term] = appendUniqueSearchTerm(normalized[term], other)
			}
		}
	}
	options.Synonyms = normalized
	return options, nil
}

func searchWeightFromEnv(name string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10 {
		return 0, fmt.Errorf("%s must be a number between 0 and 10", name)
	}
	return value, nil
}

func validateSearchSynonymTerm(value string) (string, error) {
	normalized := normalizeSearchSynonymTerm(value)
	if normalized == "" || len(normalized) > maxSearchSynonymTermLength {
		return "", fmt.Errorf("SEARCH_SYNONYMS_JSON terms must be non-empty and at most %d bytes", maxSearchSynonymTermLength)
	}
	return normalized, nil
}

func normalizeSearchSynonymTerm(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func appendUniqueSearchTerm(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func expandSearchSynonyms(query string, synonyms map[string][]string) []string {
	if len(synonyms) == 0 {
		return nil
	}
	normalized := normalizeSearchSynonymTerm(query)
	if normalized == "" {
		return nil
	}

	keys := []string{normalized}
	if fields := strings.Fields(normalized); len(fields) > 1 {
		keys = append(keys, fields...)
	}

	result := make([]string, 0)
	seen := map[string]struct{}{normalized: {}}
	for _, key := range keys {
		for _, alias := range synonyms[key] {
			if _, exists := seen[alias]; exists {
				continue
			}
			seen[alias] = struct{}{}
			result = append(result, alias)
			if len(result) >= maxSearchExpandedSynonymTerm {
				return result
			}
		}
	}
	return result
}
