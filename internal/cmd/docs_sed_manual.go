package cmd

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/docs/v1"

	"github.com/steipete/gogcli/internal/docssed"
	"github.com/steipete/gogcli/internal/ui"
)

func (c *DocsSedCmd) runManual(ctx context.Context, u *ui.UI, account, id string, expr sedExpr) error {
	docsSvc, err := docsService(ctx, account)
	if err != nil {
		return fmt.Errorf("create docs service: %w", err)
	}

	count, bulletReqs, err := c.runManualInner(ctx, docsSvc, id, expr)
	if err != nil {
		return fmt.Errorf("manual replace: %w", err)
	}

	// Apply deferred bullet requests via re-fetch to get current positions
	if len(bulletReqs) > 0 {
		if err := c.applyDeferredBullets(ctx, docsSvc, id); err != nil {
			return fmt.Errorf("apply bullets: %w", err)
		}
	}

	return sedOutputOK(ctx, u, id, sedOutputKV{"replaced", count})
}

// runManualInner is like runManual but reuses an existing docsSvc and returns count
// plus any deferred bullet requests. Bullet requests are returned separately so that
// the caller can merge consecutive same-preset bullets into a single request —
// required for Google Docs to interpret leading \t as nesting levels.
// sedMatch represents a single regex match found in the document with its replacement info.
type sedMatch struct {
	start, end int64
	oldText    string
	newText    string
	formats    []string
	image      *docssed.ImageSpec
	braceExpr  *braceExpr   // SEDMAT v3.5 brace expression
	braceSpans []*braceSpan // Inline scoping spans
}

func findDocMatches(doc *docs.Document, planner *docssed.MatchPlanner) []sedMatch {
	projection := docssed.ProjectDocument(doc)
	if projection.Legacy == nil {
		return nil
	}
	actions := planner.PlanSegment(*projection.Legacy)

	matches := make([]sedMatch, 0, len(actions))
	for _, action := range actions {
		replacement := action.Replacement
		formats := replacement.Formats
		if replacement.Brace != nil {
			formats = append(formats, braceExprToFormats(replacement.Brace)...)
		}
		matches = append(matches, sedMatch{
			start:      action.StartIndex,
			end:        action.EndIndex,
			oldText:    action.OldText,
			newText:    replacement.Text,
			formats:    formats,
			image:      replacement.Image,
			braceExpr:  replacement.Brace,
			braceSpans: replacement.BraceSpans,
		})
	}
	return matches
}

// processFootnotes handles footnote matches, each needing a two-phase create+populate approach.
func processFootnotes(ctx context.Context, docsSvc *docs.Service, id string, footnoteMatches []sedMatch) error {
	for i := len(footnoteMatches) - 1; i >= 0; i-- {
		m := footnoteMatches[i]
		fnReqs := []*docs.Request{
			{DeleteContentRange: &docs.DeleteContentRangeRequest{
				Range: &docs.Range{StartIndex: m.start, EndIndex: m.end},
			}},
			{CreateFootnote: &docs.CreateFootnoteRequest{
				Location: &docs.Location{Index: m.start},
			}},
		}
		resp, err := batchUpdate(ctx, docsSvc, id, fnReqs)
		if err != nil {
			return fmt.Errorf("create footnote: %w", err)
		}
		// Find the footnote ID from the response and insert text into it
		if resp != nil {
			for _, reply := range resp.Replies {
				if reply.CreateFootnote != nil && reply.CreateFootnote.FootnoteId != "" {
					fnID := reply.CreateFootnote.FootnoteId
					fnTextReqs := []*docs.Request{
						{InsertText: &docs.InsertTextRequest{
							Location: &docs.Location{
								Index:     1, // footnote body starts at index 1
								SegmentId: fnID,
							},
							Text: m.newText,
						}},
					}
					if _, err := batchUpdate(ctx, docsSvc, id, fnTextReqs); err != nil {
						return fmt.Errorf("populate footnote: %w", err)
					}
					break
				}
			}
		}
	}
	return nil
}

// formatRange tracks a text range that needs formatting applied after insertion.
type formatRange struct {
	start, end int64
	formats    []string
	hasTab     bool         // replacement text starts with \t (nested list item)
	braceExpr  *braceExpr   // SEDMAT v3.5 brace expression
	braceSpans []*braceSpan // Inline scoping spans
}

func (c *DocsSedCmd) runManualInner(ctx context.Context, docsSvc *docs.Service, id string, expr sedExpr) (int, []*docs.Request, error) {
	planner, err := docssed.NewMatchPlanner(semanticExpressionFromSedExpr(expr))
	if err != nil {
		return 0, nil, err
	}

	doc, err := getDoc(ctx, docsSvc, id)
	if err != nil {
		return 0, nil, fmt.Errorf("get document: %w", err)
	}

	matches := findDocMatches(doc, planner)
	if len(matches) == 0 {
		return 0, nil, nil
	}

	// Build requests in reverse order
	var requests []*docs.Request
	var formatRanges []formatRange

	// Separate footnote and image matches — they need special handling
	var footnoteMatches []sedMatch
	var imageMatches []sedMatch
	var regularMatches []sedMatch
	for _, m := range matches {
		switch {
		case containsFormat(m.formats, "footnote"):
			footnoteMatches = append(footnoteMatches, m)
		case m.image != nil:
			imageMatches = append(imageMatches, m)
		default:
			regularMatches = append(regularMatches, m)
		}
	}

	// Process image matches individually — Google Docs API cannot handle
	// DeleteContentRange + InsertInlineImage in the same batch request
	// (it fails to fetch the image URL when combined with other operations).
	for i := len(imageMatches) - 1; i >= 0; i-- {
		m := imageMatches[i]
		// First: delete the matched text
		deleteReqs := []*docs.Request{{
			DeleteContentRange: &docs.DeleteContentRangeRequest{
				Range: &docs.Range{StartIndex: m.start, EndIndex: m.end},
			},
		}}
		if _, err2 := batchUpdate(ctx, docsSvc, id, deleteReqs); err2 != nil {
			return 0, nil, fmt.Errorf("delete before image insert: %w", err2)
		}
		// Then: insert image in a separate API call
		imgReq := &docs.InsertInlineImageRequest{
			Uri:        m.image.URL,
			Location:   &docs.Location{Index: m.start},
			ObjectSize: buildImageSizeSpec(m.image),
		}
		if _, err2 := batchUpdate(ctx, docsSvc, id, []*docs.Request{{InsertInlineImage: imgReq}}); err2 != nil {
			return 0, nil, fmt.Errorf("image insert (url=%s idx=%d): %w", m.image.URL, m.start, err2)
		}
	}

	for i := len(regularMatches) - 1; i >= 0; i-- {
		m := regularMatches[i]
		requests = append(requests, &docs.Request{
			DeleteContentRange: &docs.DeleteContentRangeRequest{
				Range: &docs.Range{StartIndex: m.start, EndIndex: m.end},
			},
		})

		switch {
		case containsFormat(m.formats, "hrule"):
			// Horizontal rule: insert a newline, then style it with a bottom border
			requests = append(requests, &docs.Request{
				InsertText: &docs.InsertTextRequest{
					Location: &docs.Location{Index: m.start},
					Text:     "\n",
				},
			})
			requests = append(requests, buildHruleBorderRequest(m.start, m.start+1))
		default:
			if m.newText != "" {
				requests = append(requests, &docs.Request{
					InsertText: &docs.InsertTextRequest{
						Location: &docs.Location{Index: m.start},
						Text:     m.newText,
					},
				})
			}

			if m.newText != "" && (len(m.formats) > 0 || m.braceExpr != nil) {
				fmts := m.formats
				if containsFormat(fmts, "codeblock") {
					fmts = append(fmts, "code")
				}
				formatRanges = append(formatRanges, formatRange{
					start:      m.start,
					end:        m.start + utf16Len(m.newText),
					formats:    fmts,
					hasTab:     strings.HasPrefix(m.newText, "\t"),
					braceExpr:  m.braceExpr,
					braceSpans: m.braceSpans,
				})
			}
		}
	}

	// Add text-level formatting (bold, italic, code, super/sub, etc.)
	for _, fr := range formatRanges {
		if fr.braceExpr != nil {
			// SEDMAT v3.5 brace syntax path
			requests = append(requests, buildBraceTextStyleRequests(fr.braceExpr, fr.start, fr.end)...)
			// Handle inline scoping spans
			requests = append(requests, buildBraceInlineRequests(fr.braceSpans, fr.start)...)
		} else {
			requests = append(requests, buildTextStyleRequests(fr.formats, fr.start, fr.end)...)
		}
	}

	// Split paragraph-level requests into bullet requests (deferred) and
	// non-bullet requests (headings, blockquotes — applied immediately).
	// Bullets are deferred so the caller can merge consecutive same-preset
	// bullets into a single CreateParagraphBullets call, which is required
	// for Google Docs to interpret leading \t as nesting levels.
	var paraRequests []*docs.Request
	var deferredBullets []*docs.Request
	for _, fr := range formatRanges {
		paraEnd := fr.end + 1
		// Use brace paragraph formatting if available
		if fr.braceExpr != nil && hasBraceParagraphFormat(fr.braceExpr) {
			paraRequests = append(paraRequests, buildBraceParagraphStyleRequests(fr.braceExpr, fr.start, paraEnd)...)
		} else {
			for _, req := range buildParagraphStyleRequests(fr.formats, fr.start, paraEnd) {
				if req.CreateParagraphBullets != nil && fr.hasTab {
					// Nested bullets (have \t) are deferred so the caller can merge
					// them with adjacent L0 bullets for proper nesting.
					deferredBullets = append(deferredBullets, req)
				} else {
					paraRequests = append(paraRequests, req)
				}
			}
		}
	}

	// Phase 1: inserts, deletes, text formatting
	if _, err2 := batchUpdate(ctx, docsSvc, id, requests); err2 != nil {
		return 0, nil, fmt.Errorf("update document: %w", err2)
	}

	// Phase 2: non-bullet paragraph styles (headings, blockquotes)
	if _, err2 := batchUpdate(ctx, docsSvc, id, paraRequests); err2 != nil {
		return 0, nil, fmt.Errorf("apply paragraph styles: %w", err2)
	}

	// Handle footnotes — each needs create + populate, processed individually in reverse
	if err = processFootnotes(ctx, docsSvc, id, footnoteMatches); err != nil {
		return 0, nil, err
	}

	// Phase 3: insert page/section/column break if {+=X} or {break=X} is set.
	if err = applyBreakPhase(ctx, docsSvc, id, expr, formatRanges); err != nil {
		return 0, nil, err
	}

	// Phase 4: Apply structural features (columns, checkboxes, bookmarks, smart chips).
	// Requires re-fetching the document since text indices shifted in Phase 1.
	if expr.brace != nil && hasBraceStructuralFeatures(expr.brace) {
		freshDoc, err := getDoc(ctx, docsSvc, id)
		if err != nil {
			return 0, nil, fmt.Errorf("get doc for structural: %w", err)
		}

		// Collect all structural requests
		var allStructuralReqs []*docs.Request

		for _, fr := range formatRanges {
			if fr.braceExpr == nil {
				continue
			}

			// Get section boundaries for columns
			sectionStart, sectionEnd := buildSectionRangeForMatch(freshDoc, fr.start, fr.end)

			// Build structural requests
			colReqs, bulletReqs, anchorReqs, chipReqs := buildStructuralRequests(
				fr.braceExpr, fr.start, fr.end, sectionStart, sectionEnd,
			)

			allStructuralReqs = append(allStructuralReqs, colReqs...)
			allStructuralReqs = append(allStructuralReqs, anchorReqs...)
			allStructuralReqs = append(allStructuralReqs, chipReqs...)

			// Add checkbox bullets to deferred bullets
			deferredBullets = append(deferredBullets, bulletReqs...)
		}

		if _, err := batchUpdate(ctx, docsSvc, id, allStructuralReqs); err != nil {
			return 0, nil, fmt.Errorf("apply structural features: %w", err)
		}
	}

	return len(matches), deferredBullets, nil
}

// applyBreakPhase inserts page/section/column breaks after all text modifications.
func applyBreakPhase(ctx context.Context, docsSvc *docs.Service, id string, expr sedExpr, formatRanges []formatRange) error {
	if expr.brace == nil || !expr.brace.HasBreak || len(formatRanges) == 0 {
		return nil
	}

	freshDoc, err := getDoc(ctx, docsSvc, id)
	if err != nil {
		return fmt.Errorf("get doc for break: %w", err)
	}

	lastEnd := formatRanges[len(formatRanges)-1].end
	breakIdx := lastEnd + 1
	if freshDoc.Body != nil && len(freshDoc.Body.Content) > 0 {
		bodyEnd := freshDoc.Body.Content[len(freshDoc.Body.Content)-1].EndIndex
		if breakIdx >= bodyEnd {
			breakIdx = bodyEnd - 1
		}
	}

	breakReqs := buildBraceBreakRequests(expr.brace, breakIdx)
	if _, err := batchUpdate(ctx, docsSvc, id, breakReqs); err != nil {
		return fmt.Errorf("insert break: %w", err)
	}
	return nil
}
