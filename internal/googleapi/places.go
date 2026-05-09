package googleapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultPlacesBaseURL = "https://places.googleapis.com/v1"

type PlacesClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

type PlacesClientOption func(*PlacesClient)

func WithPlacesBaseURL(baseURL string) PlacesClientOption {
	return func(c *PlacesClient) {
		if strings.TrimSpace(baseURL) != "" {
			c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
		}
	}
}

func WithPlacesHTTPClient(client *http.Client) PlacesClientOption {
	return func(c *PlacesClient) {
		if client != nil {
			c.client = client
		}
	}
}

type Place struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	FormattedAddress string `json:"formattedAddress,omitempty"`
	GoogleMapsURI    string `json:"googleMapsUri,omitempty"`
}

type PlacesLookupOptions struct {
	LanguageCode string
	RegionCode   string
}

func NewPlacesClient(apiKey string, opts ...PlacesClientOption) *PlacesClient {
	c := &PlacesClient{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: defaultPlacesBaseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *PlacesClient) TextSearch(ctx context.Context, query string, opts PlacesLookupOptions) (*Place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty places text search")
	}

	body := map[string]string{"textQuery": query}
	if opts.LanguageCode != "" {
		body["languageCode"] = opts.LanguageCode
	}
	if opts.RegionCode != "" {
		body["regionCode"] = opts.RegionCode
	}

	var resp struct {
		Places []placeResponse `json:"places"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/places:searchText", body, "places.id,places.displayName,places.formattedAddress,places.googleMapsUri", &resp); err != nil {
		return nil, err
	}
	if len(resp.Places) == 0 {
		return nil, fmt.Errorf("no places matched %q", query)
	}
	return resp.Places[0].place(), nil
}

func (c *PlacesClient) Details(ctx context.Context, placeID string, opts PlacesLookupOptions) (*Place, error) {
	placeID = normalizePlaceID(placeID)
	if placeID == "" {
		return nil, fmt.Errorf("empty place id")
	}

	u, err := url.Parse(c.baseURL + "/places/" + url.PathEscape(placeID))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if opts.LanguageCode != "" {
		q.Set("languageCode", opts.LanguageCode)
	}
	if opts.RegionCode != "" {
		q.Set("regionCode", opts.RegionCode)
	}
	u.RawQuery = q.Encode()

	var resp placeResponse
	if err := c.doJSON(ctx, http.MethodGet, u.String(), nil, "id,displayName,formattedAddress,googleMapsUri", &resp); err != nil {
		return nil, err
	}
	place := resp.place()
	if place.ID == "" {
		place.ID = placeID
	}
	return place, nil
}

func (c *PlacesClient) doJSON(ctx context.Context, method, endpoint string, body any, fieldMask string, out any) error {
	if strings.TrimSpace(c.apiKey) == "" {
		return fmt.Errorf("missing Places API key")
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.apiKey)
	req.Header.Set("X-Goog-FieldMask", fieldMask)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return placesAPIError(resp.StatusCode, respBody)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode Places API response: %w", err)
	}
	return nil
}

type placeResponse struct {
	ID          string `json:"id"`
	DisplayName struct {
		Text string `json:"text"`
	} `json:"displayName"`
	FormattedAddress string `json:"formattedAddress"`
	GoogleMapsURI    string `json:"googleMapsUri"`
}

func (p placeResponse) place() *Place {
	return &Place{
		ID:               strings.TrimSpace(p.ID),
		Name:             strings.TrimSpace(p.DisplayName.Text),
		FormattedAddress: strings.TrimSpace(p.FormattedAddress),
		GoogleMapsURI:    strings.TrimSpace(p.GoogleMapsURI),
	}
}

func normalizePlaceID(placeID string) string {
	placeID = strings.TrimSpace(placeID)
	placeID = strings.TrimPrefix(placeID, "places/")
	return strings.TrimSpace(placeID)
}

func placesAPIError(statusCode int, body []byte) error {
	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		if parsed.Error.Status != "" {
			return fmt.Errorf("Places API error %d %s: %s", statusCode, parsed.Error.Status, parsed.Error.Message)
		}
		return fmt.Errorf("Places API error %d: %s", statusCode, parsed.Error.Message)
	}
	return fmt.Errorf("Places API error %d: %s", statusCode, strings.TrimSpace(string(body)))
}
