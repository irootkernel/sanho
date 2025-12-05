package docs

import "errors"

var (
	ErrUnknownProject    = errors.New("unknown_project")
	ErrUnknownDocsCommit = errors.New("unknown_docs_commit")
)
