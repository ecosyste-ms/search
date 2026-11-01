package search

import (
	"slices"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/ecosyste-ms/search/internal/project"

	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

type Hit struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Lexical     float64         `json:"lexical"`
	Score       float64         `json:"score"`
	Identity    *PublicIdentity `json:"identity,omitempty"`
	Public      map[string]any  `json:"public,omitempty"`
}

func indexMapping() *mapping.IndexMappingImpl {
	m := bleve.NewIndexMapping()
	m.ScoringModel = "bm25"
	m.DefaultMapping.Dynamic = false
	for _, name := range []string{"names", "descriptions", "aliases"} {
		f := bleve.NewTextFieldMapping()
		f.Analyzer = "standard"
		f.Store = false
		f.IncludeTermVectors = false
		f.IncludeInAll = false
		f.DocValues = false
		m.DefaultMapping.AddFieldMappingsAt(name, f)
	}
	keywordFields := append([]string{"exact"}, FacetFields...)
	for _, name := range keywordFields {
		f := bleve.NewKeywordFieldMapping()
		f.Store = false
		f.IncludeTermVectors = false
		f.IncludeInAll = false
		f.DocValues = false
		m.DefaultMapping.AddFieldMappingsAt(name, f)
	}
	return m
}

func Document(p project.Project, m project.Metadata) map[string]any {
	fields := map[string]any{"names": p.Names, "descriptions": p.Descriptions, "aliases": p.Aliases, "exact": project.Lower(append(slices.Clone(p.Names), p.Name))}
	for key, values := range FacetValues(p, m) {
		fields[key] = values
	}
	return fields
}

func lexicalQuery(text string) query.Query {
	return fieldLexicalQuery(text, []string{"names", "aliases", "descriptions"}, false)
}

func mappedLexicalQuery(text string, m mapping.IndexMapping) query.Query {
	fields := []string{"names", "aliases", "descriptions"}
	if m.AnalyzerNameForPath("descriptions") != "en" {
		return lexicalQuery(text)
	}
	return fieldLexicalQuery(text, fields, true)
}

func fieldLexicalQuery(text string, fields []string, analyzeFields bool) query.Query {
	if strings.TrimSpace(text) == "" {
		return bleve.NewMatchNoneQuery()
	}
	// Require every analyzed term, allowing terms to occur in different fields.
	terms := tokens(text)
	if len(terms) == 0 {
		exact := bleve.NewTermQuery(strings.ToLower(strings.TrimSpace(text)))
		exact.SetField("exact")
		return exact
	}
	must := make([]query.Query, 0, len(terms))
	for _, term := range project.Unique(terms) {
		alternatives := []query.Query{}
		for i, field := range fields {
			boost := []float64{4, 2, 1, 2}[i]
			if analyzeFields {
				q := bleve.NewMatchQuery(term)
				q.SetField(field)
				q.SetBoost(boost)
				alternatives = append(alternatives, q)
			} else {
				q := bleve.NewTermQuery(term)
				q.SetField(field)
				q.SetBoost(boost)
				alternatives = append(alternatives, q)
			}
		}
		must = append(must, bleve.NewDisjunctionQuery(alternatives...))
	}
	exact := bleve.NewTermQuery(strings.ToLower(strings.TrimSpace(text)))
	exact.SetField("exact")
	exact.SetBoost(12)
	return bleve.NewDisjunctionQuery(bleve.NewConjunctionQuery(must...), exact)
}

var analyzer = bleve.NewIndexMapping().AnalyzerNamed("standard")

func tokens(text string) []string {
	stream := analyzer.Analyze([]byte(text))
	out := make([]string, 0, len(stream))
	for _, token := range stream {
		out = append(out, string(token.Term))
	}
	return out
}

func Mapping() *mapping.IndexMappingImpl {
	m := indexMapping()
	m.DefaultMapping.Properties["descriptions"].Fields[0].Analyzer = "en"
	return m
}

var FacetFields = []string{"ecosystem", "language", "license", "activity"}

func FacetValues(p project.Project, m project.Metadata) map[string][]string {
	return map[string][]string{"ecosystem": p.Ecosystems, "language": p.Languages, "license": p.Licenses, "activity": {m.Activity}}
}
