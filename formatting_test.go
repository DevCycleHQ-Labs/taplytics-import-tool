package main

import (
	"testing"
)

func Test_NamingFormat(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"enableDarkModeColor", "enable-dark-mode-color"},
		{"userProfilePicture", "user-profile-picture"},
		{"isFeatureEnabled", "is-feature-enabled"},
		{"subscription.v2.text.overwrite", "subscription_v2_text_overwrite"},
		{"discovery.newOrderTypeUi.ios", "discovery_new-order-type-ui_ios"},
	}

	for _, testCase := range testCases {
		result := generateFeatureKey(testCase.input)
		if result != testCase.expected {
			t.Errorf("Expected %s, got %s", testCase.expected, result)
		}
	}

}
