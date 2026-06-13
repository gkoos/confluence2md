package convert

import (
	"sort"
	"strings"

	"github.com/gkoos/confluence2md/internal/confluence"
)

// CommentsToMarkdown renders comments in a plain, readable format.
// Returns an empty string when no comments are available.
func CommentsToMarkdown(comments []confluence.CommentData) string {
	if len(comments) == 0 {
		return ""
	}

	ordered := orderCommentsForDisplay(comments)

	var out strings.Builder
	out.WriteString("## Comments\n\n")

	for idx, c := range ordered {
		author := strings.TrimSpace(c.Author)
		if author == "" {
			author = strings.TrimSpace(c.AuthorID)
		}
		if author == "" {
			author = "Unknown Author"
		}

		// Format date as human-readable (e.g., "13 February 2026")
		createdStr := "unknown"
		if !c.CreatedAt.IsZero() {
			createdStr = c.CreatedAt.Format("2 January 2006")
		}

		out.WriteString(author)
		out.WriteString("\n")
		out.WriteString(createdStr)
		out.WriteString("\n")

		body := strings.TrimSpace(c.Body)
		if body == "" {
			body = "(empty comment)"
		}

		// Convert ADF JSON body to Markdown
		if rendered, err := ToMarkdown(body); err == nil && rendered != "" {
			body = rendered
		}

		out.WriteString(body)
		out.WriteString("\n")

		// Add "(edited)" note if comment was updated after creation
		if !c.UpdatedAt.IsZero() && !c.CreatedAt.IsZero() && c.UpdatedAt.After(c.CreatedAt) {
			out.WriteString("\n(edited)\n")
		}

		// Add blank line between comments, but not after the last one
		if idx < len(ordered)-1 {
			out.WriteString("\n")
		}
	}

	return strings.TrimSpace(out.String())
}

func orderCommentsForDisplay(comments []confluence.CommentData) []confluence.CommentData {
	byID := make(map[string]confluence.CommentData, len(comments))
	childrenByParent := make(map[string][]confluence.CommentData)

	for _, c := range comments {
		id := strings.TrimSpace(c.ID)
		if id != "" {
			byID[id] = c
		}

		parentID := strings.TrimSpace(c.ParentID)
		childrenByParent[parentID] = append(childrenByParent[parentID], c)
	}

	lessByTime := func(a, b confluence.CommentData) bool {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if a.CreatedAt.IsZero() {
				return false
			}
			if b.CreatedAt.IsZero() {
				return true
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	}

	for parentID := range childrenByParent {
		sort.SliceStable(childrenByParent[parentID], func(i, j int) bool {
			return lessByTime(childrenByParent[parentID][i], childrenByParent[parentID][j])
		})
	}

	result := make([]confluence.CommentData, 0, len(comments))
	seen := make(map[string]bool, len(comments))
	var appendThread func(parentID string)
	appendThread = func(parentID string) {
		for _, child := range childrenByParent[parentID] {
			id := strings.TrimSpace(child.ID)
			if id != "" {
				if seen[id] {
					continue
				}
				seen[id] = true
			}

			result = append(result, child)
			if id != "" {
				appendThread(id)
			}
		}
	}

	rootComments := make([]confluence.CommentData, 0)
	for _, c := range comments {
		parentID := strings.TrimSpace(c.ParentID)
		if parentID == "" {
			rootComments = append(rootComments, c)
			continue
		}
		if _, ok := byID[parentID]; !ok {
			rootComments = append(rootComments, c)
		}
	}

	sort.SliceStable(rootComments, func(i, j int) bool {
		return lessByTime(rootComments[i], rootComments[j])
	})

	for _, root := range rootComments {
		id := strings.TrimSpace(root.ID)
		if id != "" && seen[id] {
			continue
		}

		result = append(result, root)
		if id != "" {
			seen[id] = true
			appendThread(id)
		}
	}

	if len(result) == len(comments) {
		return result
	}

	for _, c := range comments {
		id := strings.TrimSpace(c.ID)
		if id != "" && seen[id] {
			continue
		}
		result = append(result, c)
		if id != "" {
			seen[id] = true
		}
	}

	return result
}


