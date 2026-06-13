package render

import "github.com/clawscli/claws/internal/enrichment"

// StatusField pairs a detail field label with an enrichment status.
type StatusField struct {
	Label  string
	Status enrichment.Status
}

// StatusFields renders failure status fields and reports whether any were shown.
func (d *DetailBuilder) StatusFields(fields ...StatusField) bool {
	rendered := false
	for _, field := range fields {
		if enrichment.IsFailure(field.Status) {
			d.Field(field.Label, enrichment.Display(field.Status))
			rendered = true
		}
	}
	return rendered
}

// StatusBranch renders the common failure/empty/populated detail-section branch.
func (d *DetailBuilder) StatusBranch(status enrichment.Status, hasItems bool, renderFailure func(string), renderEmpty, renderItems func()) *DetailBuilder {
	if enrichment.IsFailure(status) {
		if renderFailure != nil {
			renderFailure(enrichment.Display(status))
		}
		return d
	}
	if !hasItems {
		if renderEmpty != nil {
			renderEmpty()
		}
		return d
	}
	if renderItems != nil {
		renderItems()
	}
	return d
}

// StatusSummaryField returns a summary field for failed enrichment status.
func StatusSummaryField(label string, status enrichment.Status) (SummaryField, bool) {
	if !enrichment.IsFailure(status) {
		return SummaryField{}, false
	}
	return SummaryField{Label: label, Value: enrichment.Display(status)}, true
}
