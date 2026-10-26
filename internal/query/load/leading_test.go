package load

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

var keepMeta = LoadOptions{KeepMeta: true}

func TestLeadingContentAndSeparators(t *testing.T) {
	docs := mustReadAll(t, "# head\n%YAML 1.2\n---\na: 1\n---\nb: 2\n", keepMeta)
	wantEqual(t, len(docs), 2)
	wantEqual(t, docs[0].LeadingContent, "# head\n%YAML 1.2\n$yqDocSeparator$\n")
	wantEqual(t, docs[1].LeadingContent, "$yqDocSeparator$\n")
}

func TestLeadingCommentsAreNotRepeatedOnTheFirstKey(t *testing.T) {
	docs := mustReadAll(t, "# c\na: 1\n# b\nb: 2\n", keepMeta)
	wantEqual(t, docs[0].LeadingContent, "# c\n")
	e := docs[0].Value.Entries()
	wantEqual(t, e[0].Key.Meta().HeadComment, "")
	wantEqual(t, docs[0].Value.Meta().HeadComment, "")
	wantEqual(t, e[0].Key.Meta().Line, 1) // yq counts lines from after the leading content
	wantEqual(t, e[1].Key.Meta().HeadComment, "# b")
}

func TestLeadingContentTakesBlankLinesAndIndentedComments(t *testing.T) {
	docs := mustReadAll(t, "# h\n\n    # j\n  \r\n\na: 1\n", keepMeta)
	wantEqual(t, docs[0].LeadingContent, "# h\n\n    # j\n  \r\n\n")
	wantEqual(t, docs[0].Value.Entries()[0].Key.Meta().HeadComment, "")
	wantEqual(t, docs[0].Value.Meta().HeadComment, "")
}

func TestLeadingContentStopsAtTheFirstNode(t *testing.T) {
	wantEqual(t, mustReadAll(t, "a: 1\n", keepMeta)[0].LeadingContent, "")
	// %TAG is not leading content, so its handle still reaches the parser.
	docs := mustReadAll(t, "# c\n%TAG !e! tag:e.com,2000:\n---\na: !e!x 1\n", keepMeta)
	wantEqual(t, docs[0].LeadingContent, "# c\n")
	wantEqual(t, mustGet(docs[0].Value, "a").Meta().Tag, "tag:e.com,2000:x")
}

func TestLeadingSeparatorWithContentOnItsLine(t *testing.T) {
	// A deliberate departure from yq 4.54.1. yq drops the line break after
	// the content of a "--- <content>" line, which joins it to the next
	// line, so "--- # x\na: 1" loses .a. We keep the line break.
	docs := mustReadAll(t, "--- # x\na: 1\n", keepMeta)
	wantEqual(t, docs[0].LeadingContent, "$yqDocSeparator$\n# x\n")
	wantEqual(t, mustGet(docs[0].Value, "a").Float(), 1.0)
	docs = mustReadAll(t, "# c\n--- !!map\na: 1\n", keepMeta)
	wantEqual(t, docs[0].LeadingContent, "# c\n$yqDocSeparator$\n")
	wantEqual(t, docs[0].Value.Meta().Tag, "!!map")
}

func TestLeadingSeparatorsFormOneDocument(t *testing.T) {
	docs := mustReadAll(t, "---\n---\na: 1\n", keepMeta)
	wantEqual(t, len(docs), 1)
	wantEqual(t, docs[0].LeadingContent, "$yqDocSeparator$\n$yqDocSeparator$\n")
}

func TestLeadingContentAloneIsANullDocument(t *testing.T) {
	docs := mustReadAll(t, "# only\n", keepMeta)
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	wantEqual(t, docs[0].LeadingContent, "# only\n")
	wantEqual(t, docs[0].Value.Kind(), value.KindNull)
	wantEqual(t, len(mustReadAll(t, "", keepMeta)), 0)
}

func TestLeadingContentNeedsKeepMeta(t *testing.T) {
	docs := mustReadAll(t, "# c\n\na: 1\n---\nb: 2\n", LoadOptions{})
	wantEqual(t, docs[0].LeadingContent, "")
	wantEqual(t, docs[1].LeadingContent, "")
	wantEqual(t, len(mustReadAll(t, "---\n---\na: 1\n", LoadOptions{})), 2)
}

func TestLeadingContentIsPerSource(t *testing.T) {
	r := Chain(
		Open(Source{"f", strings.NewReader("# f\na: 1\n")}, 0, keepMeta),
		Open(Source{"g", strings.NewReader("# g\nb: 1\n")}, 1, keepMeta),
	)
	first, _ := r.Next()
	second, _ := r.Next()
	wantEqual(t, first.LeadingContent, "# f\n")
	wantEqual(t, second.LeadingContent, "# g\n")
}

func TestLeadingSeparatorsKeepTheirLineBreaks(t *testing.T) {
	// yq 4.54.1 keeps a CRLF in the first document's leading content and
	// prints "---\n" before every later document, so a later document's
	// separator ends in "\n" whatever the source used.
	docs := mustReadAll(t, "# c\r\n---\r\na: 1\r\n---\r\nb: 2\r\n", keepMeta)
	wantEqual(t, len(docs), 2)
	wantEqual(t, docs[0].LeadingContent, "# c\r\n$yqDocSeparator$\r\n")
	wantEqual(t, docs[1].LeadingContent, "$yqDocSeparator$\n")
}

func TestLeadingSeparatorWithoutALineBreak(t *testing.T) {
	// As in yq 4.54.1, a source that is only "---" is one null document
	// whose separator has no line break after it.
	docs := mustReadAll(t, "---", keepMeta)
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	wantEqual(t, docs[0].LeadingContent, "$yqDocSeparator$")
	wantEqual(t, docs[0].Value.Kind(), value.KindNull)
	docs = mustReadAll(t, "# c\n---", keepMeta)
	wantEqual(t, docs[0].LeadingContent, "# c\n$yqDocSeparator$")
}
