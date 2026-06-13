package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/docs/v1"

	"github.com/steipete/gogcli/internal/docssed"
)

func TestResolveHeading_AllValues(t *testing.T) {
	assert.Equal(t, "TITLE", resolveHeading("t"))
	assert.Equal(t, "SUBTITLE", resolveHeading("s"))
	assert.Equal(t, "HEADING_1", resolveHeading("1"))
	assert.Equal(t, "HEADING_6", resolveHeading("6"))
	assert.Equal(t, "NORMAL_TEXT", resolveHeading("0"))
	assert.Equal(t, "HEADING_3", resolveHeading("3"))
	assert.Equal(t, "CUSTOM", resolveHeading("CUSTOM"))
}

func TestBuildBraceTextStyleRequests_ImplicitReset(t *testing.T) {
	expression := &braceExpr{Bold: boolPtr(true), Indent: indentNotSet}
	requests := buildBraceTextStyleRequests(expression, 1, 10)
	require.Len(t, requests, 2)
	assert.Contains(t, requests[0].UpdateTextStyle.Fields, "bold")
	assert.Contains(t, requests[0].UpdateTextStyle.Fields, "baselineOffset")
	assert.True(t, requests[1].UpdateTextStyle.TextStyle.Bold)

	additive := &braceExpr{Bold: boolPtr(true), NoReset: true, Indent: indentNotSet}
	additiveRequests := buildBraceTextStyleRequests(additive, 1, 10)
	require.Len(t, additiveRequests, 1)
	assert.True(t, additiveRequests[0].UpdateTextStyle.TextStyle.Bold)
}

func TestClassifyMatch_BraceImage(t *testing.T) {
	expression := sedExpr{
		pattern:     "hello",
		replacement: "world",
		brace: &braceExpr{
			ImgRef: "https://example.com/img.png",
			Width:  100,
			Height: 50,
			Indent: indentNotSet,
		},
	}
	doc := &docs.Document{Body: &docs.Body{Content: []*docs.StructuralElement{{
		StartIndex: 10,
		EndIndex:   15,
		Paragraph: &docs.Paragraph{
			Elements: []*docs.ParagraphElement{{
				StartIndex: 10,
				EndIndex:   15,
				TextRun:    &docs.TextRun{Content: "hello"},
			}},
		},
	}}}}
	planner, err := docssed.NewMatchPlanner(semanticExpressionFromSedExpr(expression))
	require.NoError(t, err)
	matches := findDocMatches(doc, planner)
	require.Len(t, matches, 1)
	match := matches[0]
	assert.NotNil(t, match.image)
	assert.Equal(t, "https://example.com/img.png", match.image.URL)
	assert.Equal(t, 100, match.image.Width)
	assert.Equal(t, 50, match.image.Height)
	assert.Equal(t, int64(10), match.start)
	assert.Equal(t, int64(15), match.end)
}

func TestClassifyMatch_PlainText(t *testing.T) {
	doc := &docs.Document{Body: &docs.Body{Content: []*docs.StructuralElement{{
		StartIndex: 0,
		EndIndex:   3,
		Paragraph: &docs.Paragraph{
			Elements: []*docs.ParagraphElement{{
				StartIndex: 0,
				EndIndex:   3,
				TextRun:    &docs.TextRun{Content: "foo"},
			}},
		},
	}}}}
	planner, err := docssed.NewMatchPlanner(docssed.Expression{Pattern: "foo", Replacement: "bar"})
	require.NoError(t, err)
	matches := findDocMatches(doc, planner)
	require.Len(t, matches, 1)
	match := matches[0]
	assert.Nil(t, match.image)
	assert.Equal(t, "bar", match.newText)
}
