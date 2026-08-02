package document

const DefaultWindowBytes = 2000

type Window struct {
	Prefix string
	Suffix string
}

func AroundCursor(content string, offset int, limit int) Window {
	if limit <= 0 {
		limit = DefaultWindowBytes
	}

	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}

	prefixStart := offset - limit
	if prefixStart < 0 {
		prefixStart = 0
	}

	suffixEnd := offset + limit
	if suffixEnd > len(content) {
		suffixEnd = len(content)
	}

	return Window{
		Prefix: content[prefixStart:offset],
		Suffix: content[offset:suffixEnd],
	}
}
