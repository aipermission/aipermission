package mailcontent

import (
	"testing"

	"github.com/emersion/go-imap"
)

func TestMIMETraversalEnforcesExactDepthLimit(t *testing.T) {
	root := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed"}
	current := root
	for depth := 1; depth <= maxMIMEDepth; depth++ {
		child := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed"}
		current.Parts = []*imap.BodyStructure{child}
		current = child
	}
	visited := 0
	limited := walkBodyStructure(root, nil, func(BodyPart, int) bodyWalkDecision {
		visited++
		return bodyWalkContinue
	})
	if !limited || visited != maxMIMEDepth {
		t.Fatalf("visited=%d limited=%v", visited, limited)
	}
}

func TestMIMETraversalEnforcesPartCountLimit(t *testing.T) {
	parts := make([]*imap.BodyStructure, maxMIMEParts+1)
	for index := range parts {
		parts[index] = &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain"}
	}
	visited := 0
	limited := walkBodyStructure(&imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: parts}, nil, func(BodyPart, int) bodyWalkDecision {
		visited++
		return bodyWalkContinue
	})
	if !limited || visited != maxMIMEParts {
		t.Fatalf("visited=%d limited=%v", visited, limited)
	}
}

func TestMIMETraversalSkipsInvalidAndOverdeepBranchesWithoutDroppingSiblings(t *testing.T) {
	deep := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed"}
	current := deep
	for depth := 1; depth <= maxMIMEDepth; depth++ {
		child := &imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed"}
		current.Parts = []*imap.BodyStructure{child}
		current = child
	}
	sibling := &imap.BodyStructure{MIMEType: "text", MIMESubType: "plain"}
	visitedSibling := false
	limited := walkBodyStructure(&imap.BodyStructure{MIMEType: "multipart", MIMESubType: "mixed", Parts: []*imap.BodyStructure{deep, nil, sibling}}, nil, func(part BodyPart, _ int) bodyWalkDecision {
		if part.Structure == sibling {
			visitedSibling = true
		}
		return bodyWalkContinue
	})
	if !limited || !visitedSibling {
		t.Fatalf("limited=%v visited sibling=%v", limited, visitedSibling)
	}
}
