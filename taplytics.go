package main

import (
	"github.com/ettle/strcase"
	"strings"
)

type TLVariable struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type TLImportFormat struct {
	TLProject  string           `json:"tl_project"`
	DVCProject string           `json:"dvc_project"`
	Records    []TLImportRecord `json:"records"`
}

func (t *TLImportFormat) GetCustomDataProperties() map[string]string {
	customData := make(map[string]string)
	for _, record := range t.Records {
		for _, aud := range record.Audience.Filters.Filters {
			if aud.SubType == "customData" {
				if _, ok := customData[aud.DataKey]; !ok {
					customData[aud.DataKey] = aud.DataKeyType
				}
			}
		}
	}
	return customData
}

type TLImportRecord struct {
	ID          string       `json:"_id"`
	FeatureName string       `json:"featureName"`
	Variables   []TLVariable `json:"variables"`
	Tags        []string     `json:"tags"`
	Audience    TLAudience   `json:"audience"`
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
	Type        string `json:"type"`
	Comparator  string `json:"comparator"`
	Values      []any  `json:"values"`
	SubType     string `json:"subType"`
	DataKey     string `json:"dataKey"`
	DataKeyType string `json:"dataKeyType"`
}

func convertTLFiltersToDevCycleTargeting(tlFilter TLFilter) map[string]interface{} {
	result := map[string]interface{}{
		"operator": tlFilter.Operator,
		"filters":  []map[string]interface{}{},
	}

	for _, filter := range tlFilter.Filters {
		dvcFilter := map[string]interface{}{}

		switch filter.SubType {
		case "platform":
			dvcFilter["type"] = "user"
			dvcFilter["subType"] = "platform"
			dvcFilter["comparator"] = filter.Comparator
			dvcFilter["values"] = filter.Values
		case "appVersion":
			dvcFilter["type"] = "user"
			dvcFilter["subType"] = "appVersion"
			dvcFilter["comparator"] = filter.Comparator
			dvcFilter["values"] = filter.Values
		case "customData":
			dvcFilter["type"] = "user"
			dvcFilter["subType"] = "customData"
			dvcFilter["dataKey"] = filter.DataKey
			dvcFilter["comparator"] = filter.Comparator
			dvcFilter["values"] = filter.Values
		default:
			// Skip unsupported filter types
			continue
		}

		result["filters"] = append(result["filters"].([]map[string]interface{}), dvcFilter)
	}

	return result
}

func generateFeatureKey(name string) string {

	sections := strings.Split(name, ".")
	var modifiedSections []string
	for _, section := range sections {
		modifiedSections = append(modifiedSections, strcase.ToKebab(section))
	}

	key := strings.Join(modifiedSections, "_")

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
