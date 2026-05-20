package cmd

import (
	"context"
	"fmt"

	"google.golang.org/api/docs/v1"
)

// TableInserter handles multi-step table insertion for native Google Docs tables
type TableInserter struct {
	svc   *docs.Service
	docID string
}

func NewTableInserter(svc *docs.Service, docID string) *TableInserter {
	return &TableInserter{
		svc:   svc,
		docID: docID,
	}
}

// InsertNativeTable inserts a native Google Docs table and populates it with content
// Returns the end index of the table after insertion
func (ti *TableInserter) InsertNativeTable(ctx context.Context, tableIndex int64, cells [][]string, tabID string) (int64, error) {
	if len(cells) == 0 || len(cells[0]) == 0 {
		return tableIndex, nil
	}

	rows := int64(len(cells))
	cols := int64(len(cells[0]))

	// Step 1: Insert the table structure
	insertTableReq := &docs.Request{
		InsertTable: &docs.InsertTableRequest{
			Rows:    rows,
			Columns: cols,
			Location: &docs.Location{
				Index: tableIndex,
				TabId: tabID,
			},
		},
	}

	_, err := ti.svc.Documents.BatchUpdate(ti.docID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{insertTableReq},
	}).Context(ctx).Do()
	if err != nil {
		return tableIndex, fmt.Errorf("insert table: %w", err)
	}

	// Step 2: Fetch the document to get cell indices
	doc, err := ti.svc.Documents.Get(ti.docID).Context(ctx).Do()
	if err != nil {
		return tableIndex, fmt.Errorf("get document after table insert: %w", err)
	}

	// Step 3: Find the table in the document and get cell indices
	cellIndices, tableEndIndex, err := ti.getTableCellIndices(doc, tableIndex, rows, cols)
	if err != nil {
		return tableEndIndex, err
	}

	// Step 4: Insert text into each cell
	for rowIdx := 0; rowIdx < len(cells); rowIdx++ {
		for colIdx := 0; colIdx < len(cells[rowIdx]); colIdx++ {
			cellContent := cells[rowIdx][colIdx]
			if cellContent == "" {
				continue
			}

			cellIdx := cellIndices[rowIdx][colIdx]
			if cellIdx == 0 {
				continue
			}

			requests, insertedLen := buildTableCellRequests(cellContent, cellIdx, rowIdx == 0)
			if len(requests) == 0 {
				continue
			}

			_, err := ti.svc.Documents.BatchUpdate(ti.docID, &docs.BatchUpdateDocumentRequest{
				Requests: requests,
			}).Context(ctx).Do()
			if err != nil {
				return tableEndIndex, fmt.Errorf("insert cell text: %w", err)
			}

			// Update indices for subsequent cells (they shift by the content length)
			ti.updateIndicesAfter(cellIdx, insertedLen, cellIndices, &tableEndIndex)
		}
	}

	return tableEndIndex, nil
}

// buildTableCellRequests constructs the batch requests required to populate a
// single table cell, expanding inline markdown (**bold**, *italic*, `code`,
// [links]) into UpdateTextStyle requests on top of the inserted text. Header
// cells additionally receive a whole-cell bold style. Returns the requests and
// the UTF-16 length of the text that will be inserted so callers can keep
// running cell indices in sync. If the cell content strips to an empty string
// (e.g. content was only markers), returns (nil, 0).
func buildTableCellRequests(cellContent string, cellIdx int64, isHeaderRow bool) ([]*docs.Request, int64) {
	styles, stripped := ParseInlineFormatting(cellContent)
	if stripped == "" {
		return nil, 0
	}

	insertedLen := utf16Len(stripped)
	requests := []*docs.Request{{
		InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: cellIdx},
			Text:     stripped,
		},
	}}

	if isHeaderRow {
		requests = append(requests, &docs.Request{
			UpdateTextStyle: &docs.UpdateTextStyleRequest{
				Range: &docs.Range{
					StartIndex: cellIdx,
					EndIndex:   cellIdx + insertedLen,
				},
				TextStyle: &docs.TextStyle{Bold: true},
				Fields:    "bold",
			},
		})
	}

	for _, style := range styles {
		if req := buildTextStyleRequest(style, cellIdx, ""); req != nil {
			requests = append(requests, req)
		}
	}

	return requests, insertedLen
}

// getTableCellIndices extracts the start index for each cell in a table
func (ti *TableInserter) getTableCellIndices(doc *docs.Document, tableStartIndex int64, rows, cols int64) ([][]int64, int64, error) {
	cellIndices := make([][]int64, rows)
	for i := range cellIndices {
		cellIndices[i] = make([]int64, cols)
	}

	var tableEndIndex int64

	// Find the table in the document
	if doc.Body == nil {
		return cellIndices, tableEndIndex, fmt.Errorf("document body is nil")
	}

	// Look for table element starting near tableStartIndex
	for _, element := range doc.Body.Content {
		if element.Table != nil {
			// Check if this is our table (starts near the expected index)
			if element.StartIndex >= tableStartIndex-2 && element.StartIndex <= tableStartIndex+2 {
				tableEndIndex = element.EndIndex

				// Extract cell indices from table
				for rowIdx, row := range element.Table.TableRows {
					if rowIdx >= int(rows) {
						break
					}
					for colIdx, cell := range row.TableCells {
						if colIdx >= int(cols) {
							break
						}
						// Cell content starts at StartIndex + 1 (after the cell start marker)
						if len(cell.Content) > 0 {
							cellIndices[rowIdx][colIdx] = cell.Content[0].StartIndex
						}
					}
				}
				break
			}
		}
	}

	if tableEndIndex == 0 {
		return cellIndices, tableEndIndex, fmt.Errorf("table not found near index %d", tableStartIndex)
	}

	return cellIndices, tableEndIndex, nil
}

// updateIndicesAfter updates cell indices after text insertion
func (ti *TableInserter) updateIndicesAfter(afterIndex, length int64, cellIndices [][]int64, tableEndIndex *int64) {
	for i, row := range cellIndices {
		for j, idx := range row {
			if idx > afterIndex {
				cellIndices[i][j] = idx + length
			}
		}
	}
	if *tableEndIndex > afterIndex {
		*tableEndIndex += length
	}
}

// nextTableInsertOffset returns the running offset to apply to subsequent
// markdown-table placeholder positions after inserting a native table that
// spans [tableIndex, tableEnd). InsertTable inserts the new table before the
// existing character at tableIndex, so the placeholder "\n" we wrote into
// plainText for that table position stays in the doc; every subsequent
// placeholder therefore shifts forward by (tableEnd - tableIndex). The
// previous formula subtracted an extra 1, which accumulated one missing
// character of drift per table; see #607.
func nextTableInsertOffset(currentOffset, tableIndex, tableEnd int64) int64 {
	if tableEnd <= tableIndex {
		return currentOffset
	}
	return currentOffset + (tableEnd - tableIndex)
}
