package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/calendar/v3"
)

func TestResolveCalendarPlaceTextSearch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("GOG_PLACES_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/places:searchText" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Goog-Api-Key"); got != "test-key" {
			t.Fatalf("api key = %q", got)
		}
		if got := r.Header.Get("X-Goog-FieldMask"); !strings.Contains(got, "places.id") {
			t.Fatalf("field mask = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"places":[{"id":"ChIJ123","displayName":{"text":"Cafe"},"formattedAddress":"1 Main St","googleMapsUri":"https://maps.example/cafe"}]}`))
	}))
	defer srv.Close()
	t.Setenv("GOG_PLACES_BASE_URL", srv.URL)

	place, err := resolveCalendarPlace(context.Background(), calendarPlaceLookup{LocationSearch: "cafe"})
	if err != nil {
		t.Fatalf("resolveCalendarPlace: %v", err)
	}
	if place.ID != "ChIJ123" || place.Name != "Cafe" || place.FormattedAddress != "1 Main St" || place.GoogleMapsURI == "" {
		t.Fatalf("unexpected place: %#v", place)
	}
}

func TestResolveCalendarPlaceValidation(t *testing.T) {
	_, err := resolveCalendarPlace(context.Background(), calendarPlaceLookup{LocationSet: true, LocationSearch: "cafe"})
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestBuildCalendarCreatePlanAppliesResolvedPlace(t *testing.T) {
	plan, err := buildCalendarCreatePlan(&CalendarCreateCmd{
		CalendarID:  "primary",
		Summary:     "Coffee",
		From:        "2026-05-10T10:00:00Z",
		To:          "2026-05-10T11:00:00Z",
		SendUpdates: "none",
		resolvedPlace: &calendarPlace{
			ID:               "ChIJ123",
			Name:             "Cafe",
			FormattedAddress: "1 Main St",
			GoogleMapsURI:    "https://maps.example/cafe",
		},
	})
	if err != nil {
		t.Fatalf("buildCalendarCreatePlan: %v", err)
	}
	if plan.Event.Location != "Cafe, 1 Main St" {
		t.Fatalf("location = %q", plan.Event.Location)
	}
	props := plan.Event.ExtendedProperties
	if props == nil || props.Private[placeIDPrivateProp] != "ChIJ123" || props.Private[placeMapsURIPrivateProp] == "" {
		t.Fatalf("unexpected place props: %#v", props)
	}
}

func TestApplyCalendarPlacePropertiesMerges(t *testing.T) {
	event := &calendar.Event{ExtendedProperties: buildExtendedProperties([]string{"existing=value"}, nil)}
	applyCalendarPlaceProperties(event, &calendarPlace{ID: "ChIJ123"})

	if event.ExtendedProperties.Private["existing"] != "value" {
		t.Fatalf("existing private prop lost: %#v", event.ExtendedProperties.Private)
	}
	if event.ExtendedProperties.Private[placeIDPrivateProp] != "ChIJ123" {
		t.Fatalf("place private prop missing: %#v", event.ExtendedProperties.Private)
	}
}
