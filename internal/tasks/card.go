package tasks

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Card is the in-memory form of one TASK-XXXX.md file: frontmatter fields
// plus an ordered list of Markdown sections. Parsing and serializing a Card
// without changing any field must be byte-identical (see card_test.go) so
// that git diffs on a card only ever show the field that actually changed.
type Card struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      Type      `json:"type"`
	Status    Status    `json:"status"`
	Priority  Priority  `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ImpactedAreas are free-form module/path hints for humans; RegistryRefs
	// are the stable identifiers (entry:<entry_id>, module:<name>, or
	// query:<saved-query-id>) the Registry resolves against real paths.
	ImpactedAreas []string `json:"impacted_areas"`
	RegistryRefs  []string `json:"registry_refs"`
	Dependencies  []string `json:"dependencies"`

	// Sections holds every "## Heading" body block in file order. Known
	// headings (sectionOrder) are always present, even if empty, so a freshly
	// created card has a stable, predictable shape.
	Sections []Section `json:"sections"`
}

// Section is one "## Heading" Markdown block.
type Section struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

func (c *Card) section(heading string) string {
	for _, s := range c.Sections {
		if s.Heading == heading {
			return s.Body
		}
	}
	return ""
}

func (c *Card) setSection(heading, body string) {
	for i := range c.Sections {
		if c.Sections[i].Heading == heading {
			c.Sections[i].Body = body
			return
		}
	}
	c.Sections = append(c.Sections, Section{Heading: heading, Body: body})
}

// NewCard builds a fresh card with every known section present and empty.
func NewCard(id, title string, taskType Type, priority Priority, now time.Time) Card {
	card := Card{
		ID:        id,
		Title:     strings.TrimSpace(title),
		Type:      taskType,
		Status:    StatusIdea,
		Priority:  priority,
		CreatedAt: now,
		UpdatedAt: now,
	}
	for _, heading := range sectionOrder {
		card.Sections = append(card.Sections, Section{Heading: heading})
	}
	return card
}

// Validate checks the fields a card must always carry to be a legal
// .shproject/tasks/cards entry, independent of workflow transitions.
func (c Card) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(c.Title) == "" {
		return fmt.Errorf("task title is required")
	}
	if strings.TrimSpace(string(c.Type)) == "" {
		return fmt.Errorf("task type is required")
	}
	if !c.Status.valid() {
		return fmt.Errorf("unknown task status %q", c.Status)
	}
	if !c.Priority.valid() {
		return fmt.Errorf("unknown task priority %q", c.Priority)
	}
	if c.CreatedAt.IsZero() {
		return fmt.Errorf("task created_at is required")
	}
	if c.UpdatedAt.IsZero() {
		return fmt.Errorf("task updated_at is required")
	}
	return nil
}

const timeLayout = time.RFC3339

// Marshal serializes a Card back to the exact Markdown card format.
func (c Card) Marshal() []byte {
	var b strings.Builder
	b.WriteString("---\n")
	writeScalar(&b, "id", c.ID)
	writeScalar(&b, "title", c.Title)
	writeScalar(&b, "type", string(c.Type))
	writeScalar(&b, "status", string(c.Status))
	writeScalar(&b, "priority", string(c.Priority))
	writeScalar(&b, "created_at", c.CreatedAt.UTC().Format(timeLayout))
	writeScalar(&b, "updated_at", c.UpdatedAt.UTC().Format(timeLayout))
	writeList(&b, "impacted_areas", c.ImpactedAreas)
	writeList(&b, "registry_refs", c.RegistryRefs)
	writeList(&b, "dependencies", c.Dependencies)
	b.WriteString("---\n")

	known := make(map[string]bool, len(sectionOrder))
	for _, heading := range sectionOrder {
		known[heading] = true
		b.WriteString("\n## ")
		b.WriteString(heading)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimRight(c.section(heading), "\n"))
		b.WriteString("\n")
	}
	for _, s := range c.Sections {
		if known[s.Heading] {
			continue
		}
		b.WriteString("\n## ")
		b.WriteString(s.Heading)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimRight(s.Body, "\n"))
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func writeScalar(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "%s: %s\n", key, scalarValue(value))
}

// scalarValue quotes a value only when leaving it bare would change its
// parsed meaning (empty, or starting with a character our own reader treats
// specially). Plain identifiers and titles stay unquoted for readability.
func scalarValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, "#:") || strings.HasPrefix(value, "- ") ||
		strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "[") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func writeList(b *strings.Builder, key string, values []string) {
	values = normalizeStringList(values)
	if len(values) == 0 {
		fmt.Fprintf(b, "%s: []\n", key)
		return
	}
	fmt.Fprintf(b, "%s:\n", key)
	for _, v := range values {
		fmt.Fprintf(b, "  - %s\n", scalarValue(v))
	}
}

// ParseCard reads the exact format Marshal produces. It is intentionally not
// a general YAML parser: only scalar strings and "- item" lists are
// supported, which is all a task card ever needs.
func ParseCard(data []byte) (Card, error) {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Card{}, fmt.Errorf("card must start with a --- frontmatter block")
	}
	i := 1
	fields := map[string]string{}
	lists := map[string][]string{}
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			i++
			break
		}
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			return Card{}, fmt.Errorf("malformed frontmatter line %q", line)
		}
		key = strings.TrimSpace(key)
		rest = strings.TrimSpace(rest)
		if rest == "" {
			// Block list form.
			var items []string
			for i+1 < len(lines) {
				candidate := lines[i+1]
				trimmed := strings.TrimSpace(candidate)
				if !strings.HasPrefix(candidate, "  ") || !strings.HasPrefix(trimmed, "- ") {
					break
				}
				items = append(items, unquote(strings.TrimPrefix(trimmed, "- ")))
				i++
			}
			lists[key] = items
		} else if rest == "[]" {
			lists[key] = []string{}
		} else {
			fields[key] = unquote(rest)
		}
		i++
	}

	createdAt, err := parseTime(fields["created_at"])
	if err != nil {
		return Card{}, fmt.Errorf("created_at: %w", err)
	}
	updatedAt, err := parseTime(fields["updated_at"])
	if err != nil {
		return Card{}, fmt.Errorf("updated_at: %w", err)
	}

	card := Card{
		ID:            fields["id"],
		Title:         fields["title"],
		Type:          Type(fields["type"]),
		Status:        Status(fields["status"]),
		Priority:      Priority(fields["priority"]),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ImpactedAreas: lists["impacted_areas"],
		RegistryRefs:  lists["registry_refs"],
		Dependencies:  lists["dependencies"],
	}

	body := strings.Join(lines[i:], "\n")
	card.Sections = parseSections(body)
	return card, nil
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("value is required")
	}
	return time.Parse(timeLayout, value)
}

func parseSections(body string) []Section {
	lines := strings.Split(body, "\n")
	var sections []Section
	var current *Section
	var buf []string
	flush := func() {
		if current != nil {
			current.Body = strings.Trim(strings.Join(buf, "\n"), "\n")
			sections = append(sections, *current)
		}
		buf = nil
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			current = &Section{Heading: heading}
			continue
		}
		if current != nil {
			buf = append(buf, line)
		}
	}
	flush()
	return sections
}

func unquote(value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		inner := value[1 : len(value)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	}
	return value
}

// SortByID orders cards by their numeric TASK-XXXX suffix, falling back to a
// plain string comparison for non-standard IDs.
func SortByID(cards []Card) {
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
}
