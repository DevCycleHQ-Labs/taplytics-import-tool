package main

import (
	"github.com/ettle/strcase"
	"strings"
)

type TLVariable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}
type TLImportFormat struct {
	TLProject  string           `json:"tl_project"`
	DVCProject string           `json:"dvc_project"`
	Records    []TLImportRecord `json:"records"`
}

func (t *TLImportFormat) GetCustomDataProperties() map[string]string {
	customData := make(map[string]string)
	for _, record := range t.Records {
		for _, target := range record.Targets {
			for _, aud := range target.Audience.Filters.Filters {
				if aud.SubType == "customData" && aud.DataKey != "" {
					if _, ok := customData[aud.DataKey]; !ok {
						customData[aud.DataKey] = aud.DataKeyType
					}
				}
			}
		}
	}
	return customData
}

type TLImportRecord struct {
	ID           string           `json:"_id"`
	FeatureName  string           `json:"featureName"`
	Variations   []TLVariation    `json:"variations"`
	Tags         []string         `json:"tags"`
	Targets      []TLTarget       `json:"targets"`
	Distribution []TLDistribution `json:"distribution"`
}

type TLDistribution struct {
	Name       string  `json:"name"`
	Percentage float64 `json:"percentage"`
}

func (d TLDistribution) ToAPIDistribution() map[string]interface{} {
	return map[string]interface{}{
		"_variation": toKey(d.Name),
		"percentage": d.Percentage,
	}
}

type TLVariation struct {
	Name         string       `json:"name"`
	Variables    []TLVariable `json:"variables"`
	Distribution float64      `json:"distribution"`
}

type TLTarget struct {
	Name         string           `json:"name"`
	Audience     TLAudience      `json:"audience"`
	Distribution []TLDistribution `json:"distribution"`
}

type TLAudience struct {
	Name    string   `json:"name"`
	Filters TLFilter `json:"filters"`
}

type TLFilter struct {
	Operator string         `json:"operator"`
	Filters  []TLFilterItem `json:"filters"`
}

type TLFilterItem struct {
	Type        string `json:"type,omitempty"`
	Comparator  string `json:"comparator,omitempty"`
	Values      []any  `json:"values,omitempty"`
	SubType     string `json:"subType,omitempty"`
	DataKey     string `json:"dataKey,omitempty"`
	DataKeyType string `json:"dataKeyType,omitempty"`
}

func convertTLFiltersToDevCycleTargeting(tlFilter TLFilter) map[string]interface{} {
	result := map[string]interface{}{
		"operator": tlFilter.Operator,
		"filters":  []map[string]interface{}{},
	}

	for _, filter := range tlFilter.Filters {
		dvcFilter := map[string]interface{}{}

		switch filter.SubType {
		case "appVersion":
			dvcFilter["type"] = "user"
			dvcFilter["subType"] = filter.SubType
			dvcFilter["comparator"] = filter.Comparator
			fixedValues := make([]string, len(filter.Values))
			for i, v := range filter.Values {
				if str, ok := v.(string); ok {
					if len(strings.Split(str, ".")) == 2 {
						str += ".0" // Ensure it has a patch version
					}
					fixedValues[i] = str
				}
			}
			dvcFilter["values"] = filter.Values
		case "customData":
			dvcFilter["type"] = "user"
			dvcFilter["subType"] = "customData"
			dvcFilter["dataKey"] = filter.DataKey
			dvcFilter["comparator"] = filter.Comparator
			dvcFilter["values"] = filter.Values
		default:
			dvcFilter["type"] = "user"
			dvcFilter["subType"] = filter.SubType
			dvcFilter["comparator"] = filter.Comparator
			dvcFilter["values"] = filter.Values
			continue
		}

		result["filters"] = append(result["filters"].([]map[string]interface{}), dvcFilter)
	}

	return result
}

func toKey(name string) string {
	sections := strings.Split(name, ".")
	var modifiedSections []string
	for _, section := range sections {
		modifiedSections = append(modifiedSections, strcase.ToKebab(section))
	}
	key := strings.Join(modifiedSections, "_")
	// replace all non-alphanumeric characters with empty string; allowing alphanumeric characters, hyphens, periods, and underscores
	key = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			return r
		}
		return -1 // remove the character
	}, key)
	return key
}

func convertTaplyticsVarTypeToDevCycle(tlType string) string {
	switch strings.ToLower(tlType) {
	case "string":
		return "String"
	case "number":
		return "Number"
	case "boolean":
		return "Boolean"
	case "json":
		return "JSON"
	default:
		return "String" // Default to String
	}
}
