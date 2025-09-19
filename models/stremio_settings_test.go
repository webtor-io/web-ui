package models

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStremioSettingsData_Value(t *testing.T) {
	t.Run("marshal normal data", func(t *testing.T) {
		data := StremioSettingsData{
			PreferredQualities: []QualitySetting{
				{Quality: "4k", Enabled: true},
				{Quality: "1080p", Enabled: false},
			},
		}

		value, err := data.Value()
		require.NoError(t, err)

		// Should return JSON bytes
		jsonBytes, ok := value.([]byte)
		require.True(t, ok, "Value() should return []byte")

		// Verify the JSON content
		var result StremioSettingsData
		err = json.Unmarshal(jsonBytes, &result)
		require.NoError(t, err)
		assert.Equal(t, data, result)
	})

	t.Run("marshal empty data", func(t *testing.T) {
		data := StremioSettingsData{}

		value, err := data.Value()
		require.NoError(t, err)

		jsonBytes, ok := value.([]byte)
		require.True(t, ok)

		expectedJSON := `{"preferred_qualities":null}`
		assert.JSONEq(t, expectedJSON, string(jsonBytes))
	})

	t.Run("marshal with empty slice", func(t *testing.T) {
		data := StremioSettingsData{
			PreferredQualities: []QualitySetting{},
		}

		value, err := data.Value()
		require.NoError(t, err)

		jsonBytes, ok := value.([]byte)
		require.True(t, ok)

		expectedJSON := `{"preferred_qualities":[]}`
		assert.JSONEq(t, expectedJSON, string(jsonBytes))
	})

	t.Run("marshal default settings", func(t *testing.T) {
		data := *GetDefaultStremioSettings()

		value, err := data.Value()
		require.NoError(t, err)

		jsonBytes, ok := value.([]byte)
		require.True(t, ok)

		// Verify it contains all default qualities
		var result StremioSettingsData
		err = json.Unmarshal(jsonBytes, &result)
		require.NoError(t, err)
		assert.Len(t, result.PreferredQualities, 4)
		assert.Equal(t, data, result)
	})
}

func TestStremioSettingsData_Scan(t *testing.T) {
	t.Run("scan from []byte", func(t *testing.T) {
		jsonData := `{"preferred_qualities":[{"quality":"4k","enabled":true},{"quality":"1080p","enabled":false}]}`
		var data StremioSettingsData

		err := data.Scan([]byte(jsonData))
		require.NoError(t, err)

		assert.Len(t, data.PreferredQualities, 2)
		assert.Equal(t, "4k", data.PreferredQualities[0].Quality)
		assert.True(t, data.PreferredQualities[0].Enabled)
		assert.Equal(t, "1080p", data.PreferredQualities[1].Quality)
		assert.False(t, data.PreferredQualities[1].Enabled)
	})

	t.Run("scan from string", func(t *testing.T) {
		jsonData := `{"preferred_qualities":[{"quality":"720p","enabled":true}]}`
		var data StremioSettingsData

		err := data.Scan(jsonData)
		require.NoError(t, err)

		assert.Len(t, data.PreferredQualities, 1)
		assert.Equal(t, "720p", data.PreferredQualities[0].Quality)
		assert.True(t, data.PreferredQualities[0].Enabled)
	})

	t.Run("scan nil value", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan(nil)
		require.NoError(t, err)

		// Should remain zero value
		assert.Nil(t, data.PreferredQualities)
	})

	t.Run("scan empty json", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan(`{}`)
		require.NoError(t, err)

		assert.Nil(t, data.PreferredQualities)
	})

	t.Run("scan empty json with null field", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan(`{"preferred_qualities":null}`)
		require.NoError(t, err)

		assert.Nil(t, data.PreferredQualities)
	})

	t.Run("scan empty json with empty array", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan(`{"preferred_qualities":[]}`)
		require.NoError(t, err)

		assert.NotNil(t, data.PreferredQualities)
		assert.Len(t, data.PreferredQualities, 0)
	})

	t.Run("scan invalid input type", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan(123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot scan StremioSettingsData from non-string/non-bytes value")
	})

	t.Run("scan malformed json", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan(`{"preferred_qualities":[invalid json}`)
		assert.Error(t, err)
		// Should be a JSON unmarshal error
		assert.Contains(t, err.Error(), "invalid character")
	})

	t.Run("scan json with wrong structure", func(t *testing.T) {
		var data StremioSettingsData

		// This should not cause an error but will result in zero values for missing fields
		err := data.Scan(`{"wrong_field":"value"}`)
		require.NoError(t, err)

		assert.Nil(t, data.PreferredQualities)
	})
}

func TestStremioSettingsData_RoundTrip(t *testing.T) {
	t.Run("marshal then unmarshal preserves data", func(t *testing.T) {
		original := StremioSettingsData{
			PreferredQualities: []QualitySetting{
				{Quality: "4k", Enabled: true},
				{Quality: "1080p", Enabled: false},
				{Quality: "720p", Enabled: true},
				{Quality: "other", Enabled: false},
			},
		}

		// Marshal
		value, err := original.Value()
		require.NoError(t, err)

		// Unmarshal
		var restored StremioSettingsData
		err = restored.Scan(value)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, original, restored)
	})

	t.Run("marshal then unmarshal default settings", func(t *testing.T) {
		original := *GetDefaultStremioSettings()

		// Marshal
		value, err := original.Value()
		require.NoError(t, err)

		// Unmarshal
		var restored StremioSettingsData
		err = restored.Scan(value)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, original, restored)
	})

	t.Run("marshal then unmarshal empty settings", func(t *testing.T) {
		original := StremioSettingsData{
			PreferredQualities: []QualitySetting{},
		}

		// Marshal
		value, err := original.Value()
		require.NoError(t, err)

		// Unmarshal
		var restored StremioSettingsData
		err = restored.Scan(value)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, original, restored)
	})
}

func TestStremioSettingsData_EdgeCases(t *testing.T) {
	t.Run("unicode and special characters in quality names", func(t *testing.T) {
		data := StremioSettingsData{
			PreferredQualities: []QualitySetting{
				{Quality: "4K UHD 🎬", Enabled: true},
				{Quality: "HD \"Premium\"", Enabled: false},
				{Quality: "标清", Enabled: true}, // Chinese characters
			},
		}

		// Round trip test
		value, err := data.Value()
		require.NoError(t, err)

		var restored StremioSettingsData
		err = restored.Scan(value)
		require.NoError(t, err)

		assert.Equal(t, data, restored)
	})

	t.Run("large number of quality settings", func(t *testing.T) {
		qualities := make([]QualitySetting, 100)
		for i := 0; i < 100; i++ {
			qualities[i] = QualitySetting{
				Quality: fmt.Sprintf("quality_%d", i),
				Enabled: i%2 == 0,
			}
		}

		data := StremioSettingsData{
			PreferredQualities: qualities,
		}

		// Round trip test
		value, err := data.Value()
		require.NoError(t, err)

		var restored StremioSettingsData
		err = restored.Scan(value)
		require.NoError(t, err)

		assert.Equal(t, data, restored)
		assert.Len(t, restored.PreferredQualities, 100)
	})

	t.Run("scan from empty byte slice", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan([]byte{})
		assert.Error(t, err)
		// Should be a JSON unmarshal error for empty input
	})

	t.Run("scan from empty string", func(t *testing.T) {
		var data StremioSettingsData

		err := data.Scan("")
		assert.Error(t, err)
		// Should be a JSON unmarshal error for empty input
	})
}

func TestQualitySetting_JSONSerialization(t *testing.T) {
	t.Run("quality setting json tags", func(t *testing.T) {
		quality := QualitySetting{
			Quality: "1080p",
			Enabled: true,
		}

		jsonBytes, err := json.Marshal(quality)
		require.NoError(t, err)

		expectedJSON := `{"quality":"1080p","enabled":true}`
		assert.JSONEq(t, expectedJSON, string(jsonBytes))

		// Test unmarshal
		var restored QualitySetting
		err = json.Unmarshal(jsonBytes, &restored)
		require.NoError(t, err)
		assert.Equal(t, quality, restored)
	})
}
