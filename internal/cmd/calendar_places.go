package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"google.golang.org/api/calendar/v3"

	"github.com/steipete/gogcli/internal/config"
	"github.com/steipete/gogcli/internal/googleapi"
)

const (
	placeIDPrivateProp      = "gog.place_id"
	placeMapsURIPrivateProp = "gog.place_maps_uri"
)

type calendarPlace struct {
	ID               string
	Name             string
	FormattedAddress string
	GoogleMapsURI    string
}

func (c *CalendarCreateCmd) resolvePlace(ctx context.Context) error {
	place, err := resolveCalendarPlace(ctx, calendarPlaceLookup{
		LocationSet:    strings.TrimSpace(c.Location) != "",
		LocationSearch: c.LocationSearch,
		PlaceID:        c.PlaceID,
		LanguageCode:   c.PlaceLanguage,
		RegionCode:     c.PlaceRegion,
	})
	if err != nil {
		return err
	}
	c.resolvedPlace = place
	return nil
}

func (c *CalendarUpdateCmd) resolvePlace(ctx context.Context, kctx *kong.Context) error {
	place, err := resolveCalendarPlace(ctx, calendarPlaceLookup{
		LocationSet:       flagProvided(kctx, "location"),
		LocationSearch:    c.LocationSearch,
		LocationSearchSet: flagProvided(kctx, "location-search"),
		PlaceID:           c.PlaceID,
		PlaceIDSet:        flagProvided(kctx, "place-id"),
		LanguageCode:      c.PlaceLanguage,
		RegionCode:        c.PlaceRegion,
	})
	if err != nil {
		return err
	}
	c.resolvedPlace = place
	return nil
}

type calendarPlaceLookup struct {
	LocationSet       bool
	LocationSearch    string
	LocationSearchSet bool
	PlaceID           string
	PlaceIDSet        bool
	LanguageCode      string
	RegionCode        string
}

func resolveCalendarPlace(ctx context.Context, lookup calendarPlaceLookup) (*calendarPlace, error) {
	search := strings.TrimSpace(lookup.LocationSearch)
	placeID := strings.TrimSpace(lookup.PlaceID)
	searchSet := lookup.LocationSearchSet || search != ""
	placeIDSet := lookup.PlaceIDSet || placeID != ""

	if searchSet && search == "" {
		return nil, usage("empty --location-search")
	}
	if placeIDSet && placeID == "" {
		return nil, usage("empty --place-id")
	}
	if search != "" && placeID != "" {
		return nil, usage("use either --location-search or --place-id, not both")
	}
	if lookup.LocationSet && (search != "" || placeID != "") {
		return nil, usage("cannot combine --location with --location-search or --place-id")
	}
	if search == "" && placeID == "" {
		return nil, nil //nolint:nilnil // no lookup requested
	}

	apiKey, err := placesAPIKey()
	if err != nil {
		return nil, err
	}
	client := googleapi.NewPlacesClient(apiKey, googleapi.WithPlacesBaseURL(os.Getenv("GOG_PLACES_BASE_URL")))
	opts := googleapi.PlacesLookupOptions{
		LanguageCode: strings.TrimSpace(lookup.LanguageCode),
		RegionCode:   strings.TrimSpace(lookup.RegionCode),
	}

	var place *googleapi.Place
	if search != "" {
		place, err = client.TextSearch(ctx, search, opts)
	} else {
		place, err = client.Details(ctx, placeID, opts)
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(place.ID) == "" && placeID != "" {
		place.ID = strings.TrimPrefix(placeID, "places/")
	}
	return &calendarPlace{
		ID:               strings.TrimSpace(place.ID),
		Name:             strings.TrimSpace(place.Name),
		FormattedAddress: strings.TrimSpace(place.FormattedAddress),
		GoogleMapsURI:    strings.TrimSpace(place.GoogleMapsURI),
	}, nil
}

func placesAPIKey() (string, error) {
	cfg, err := config.ReadConfig()
	if err != nil {
		return "", fmt.Errorf("read config for Places API key: %w", err)
	}
	if key := strings.TrimSpace(config.GetValue(cfg, config.KeyPlacesAPIKey)); key != "" {
		return key, nil
	}
	return "", usage("Places API key required for --location-search or --place-id. Set GOG_PLACES_API_KEY, GOOGLE_PLACES_API_KEY, or run 'gog config set places_api_key <key>'")
}

func formatCalendarPlaceLocation(place *calendarPlace) string {
	if place == nil {
		return ""
	}
	name := strings.TrimSpace(place.Name)
	address := strings.TrimSpace(place.FormattedAddress)
	switch {
	case name != "" && address != "":
		return name + ", " + address
	case name != "":
		return name
	case address != "":
		return address
	default:
		return strings.TrimSpace(place.ID)
	}
}

func applyCalendarPlaceProperties(event *calendar.Event, place *calendarPlace) {
	if event == nil || place == nil {
		return
	}
	if event.ExtendedProperties == nil {
		event.ExtendedProperties = &calendar.EventExtendedProperties{}
	}
	if event.ExtendedProperties.Private == nil {
		event.ExtendedProperties.Private = map[string]string{}
	}
	if id := strings.TrimSpace(place.ID); id != "" {
		event.ExtendedProperties.Private[placeIDPrivateProp] = id
	}
	if mapsURI := strings.TrimSpace(place.GoogleMapsURI); mapsURI != "" {
		event.ExtendedProperties.Private[placeMapsURIPrivateProp] = mapsURI
	}
}
