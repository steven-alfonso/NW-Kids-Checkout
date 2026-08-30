package planningcenter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_defaultClient_GetCheckoutsForEvent_Real(t *testing.T) {
	t.Skip()
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_BASE_URL"), "PLANNING_CENTER_API_BASE_URL must be set to run this test")
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_CLIENT_ID"), "PLANNING_CENTER_API_CLIENT_ID must be set to run this test")
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_SECRET"), "PLANNING_CENTER_API_SECRET must be set to run this test")

	type fields struct {
		baseURL  string
		clientID string
		secret   string
	}
	type args struct {
		ctx                 context.Context
		eventID             string
		checkedOutOnOrAfter time.Time
		limit               int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []Checkout
		wantErr bool
	}{
		{
			name: "get checkins",
			args: args{
				ctx:                 t.Context(),
				eventID:             "151353",
				checkedOutOnOrAfter: time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &defaultClient{
				httpClient: http.DefaultClient,
				baseURL:    os.Getenv("PLANNING_CENTER_API_BASE_URL"),
				clientID:   os.Getenv("PLANNING_CENTER_API_CLIENT_ID"),
				secret:     os.Getenv("PLANNING_CENTER_API_SECRET"),
			}
			got, err := client.GetCheckoutsForEvent(tt.args.ctx, tt.args.eventID, tt.args.checkedOutOnOrAfter, tt.args.limit)
			require.NoError(t, err)
			fmt.Printf("%+v\n", got)
			// t.Fail()
		})
	}
}

func Test_defaultClient_GetCheckoutsForEvent_Fake(t *testing.T) {
	checkedOutAt := time.Now().UTC().Add(-1 * time.Minute).Round(time.Second)
	var requestCount atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"links":{"self":"%s","next":""},"data":[{"type":"check_in","id":"123","attributes":{"first_name":"Test","last_name":"User","security_code":"ABC123","checked_out_at":"%s"}}]}`,
			server.URL+r.URL.Path,
			checkedOutAt.Format(time.RFC3339),
		)
	}))
	defer server.Close()

	client := &defaultClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
		clientID:   "client",
		secret:     "secret",
	}

	checkouts, err := client.GetCheckoutsForEvent(t.Context(), "evt-123", checkedOutAt.Add(-10*time.Minute), 0)
	require.NoError(t, err)
	require.Len(t, checkouts, 1)
	assert.Equal(t, "123", checkouts[0].ID)
	assert.Equal(t, "Test", checkouts[0].FirstName)
	assert.Equal(t, "User", checkouts[0].LastName)
	assert.Equal(t, "ABC123", checkouts[0].SecurityCode)
	assert.Equal(t, checkedOutAt, checkouts[0].CheckedOutAt)
	assert.Equal(t, int64(1), requestCount.Load())
}

func Test_defaultClient_GetCheckoutsForEvent_paginatesBeyondLegacyTenPageCap(t *testing.T) {
	const (
		rowsPerPage = 25
		totalPages  = 12 // > 10, the legacy silent cap, ~250 checkouts
		totalRows   = rowsPerPage * totalPages
	)
	checkedOutAt := time.Now().UTC().Add(-5 * time.Minute).Round(time.Second)
	var requestCount atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, _ = strconv.Atoi(raw)
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)

		next := ""
		if page < totalPages {
			next = fmt.Sprintf("%s/check-ins/v2/events/evt-123/check_ins?page=%d", server.URL, page+1)
		}

		rows := make([]string, 0, rowsPerPage)
		for i := range rowsPerPage {
			rows = append(rows, fmt.Sprintf(`{"type":"check_in","id":"%d_%d","attributes":{"first_name":"Test","last_name":"User","security_code":"ABC123","checked_out_at":"%s"}}`,
				page, i, checkedOutAt.Format(time.RFC3339)))
		}

		requestCount.Add(1)
		_, _ = fmt.Fprintf(w, `{"links":{"self":"%s","next":%q},"data":[%s]}`,
			server.URL+r.URL.Path, next, strings.Join(rows, ","))
	}))
	defer server.Close()

	client := &defaultClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
		clientID:   "client",
		secret:     "secret",
	}

	checkouts, err := client.GetCheckoutsForEvent(t.Context(), "evt-123", checkedOutAt.Add(-1*time.Hour), 0)
	require.NoError(t, err)
	require.Len(t, checkouts, totalRows, "checkouts beyond the first ten pages must not be silently dropped")
	assert.Equal(t, int64(totalPages), requestCount.Load(), "every page must be fetched until the API's next link is empty")
	assert.Equal(t, "1_0", checkouts[0].ID)
	assert.Equal(t, "12_24", checkouts[len(checkouts)-1].ID, "the last row of the final page should be present")
}

func Test_defaultClient_GetCheckoutsForEvent_nonTerminatingPagination_returnsError(t *testing.T) {
	checkedOutAt := time.Now().UTC().Add(-5 * time.Minute).Round(time.Second)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)

		// next always points back at the same URL: the API never terminates.
		_, _ = fmt.Fprintf(w, `{"links":{"next":"%s/check-ins/v2/events/evt-123/check_ins?page=1"},"data":[{"type":"check_in","id":"1_0","attributes":{"checked_out_at":"%s"}}]}`,
			server.URL, checkedOutAt.Format(time.RFC3339))
	}))
	defer server.Close()

	client := &defaultClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
		clientID:   "client",
		secret:     "secret",
	}

	_, err := client.GetCheckoutsForEvent(t.Context(), "evt-123", checkedOutAt.Add(-1*time.Hour), 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaginationLimitExceeded, "truncation must be surfaced as an explicit error, not a silent return")
}

func Test_defaultClient_GetLocation_Real(t *testing.T) {
	t.Skip()
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_BASE_URL"), "PLANNING_CENTER_API_BASE_URL must be set to run this test")
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_CLIENT_ID"), "PLANNING_CENTER_API_CLIENT_ID must be set to run this test")
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_SECRET"), "PLANNING_CENTER_API_SECRET must be set to run this test")

	type fields struct {
		baseURL  string
		clientID string
		secret   string
	}
	type args struct {
		ctx                        context.Context
		locationID                 string
		includeAssociatedLocations bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []Checkout
		wantErr bool
	}{
		{
			name: "get checkins",
			args: args{
				ctx:        t.Context(),
				locationID: "295939",
				//locationID: "723452",
				includeAssociatedLocations: true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &defaultClient{
				httpClient: http.DefaultClient,
				baseURL:    os.Getenv("PLANNING_CENTER_API_BASE_URL"),
				clientID:   os.Getenv("PLANNING_CENTER_API_CLIENT_ID"),
				secret:     os.Getenv("PLANNING_CENTER_API_SECRET"),
			}
			got, err := client.GetLocation(tt.args.ctx, tt.args.locationID, tt.args.includeAssociatedLocations)
			require.NoError(t, err)
			fmt.Printf("%+v\n", got)
		})
	}
}

func Test_defaultClient_GetEventByID(t *testing.T) {
	var requestCount atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"data":{"id":"pc_evt_22","type":"event","attributes":{"name":"Kids Service"}}}`)
	}))
	defer server.Close()

	client := &defaultClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
		clientID:   "client",
		secret:     "secret",
	}

	got, err := client.GetEventByID(t.Context(), "pc_evt_22")
	require.NoError(t, err)
	assert.Equal(t, "pc_evt_22", got.ID)
	assert.Equal(t, "Kids Service", got.Name)
	assert.Equal(t, int64(1), requestCount.Load())
}

func Test_defaultClient_GetEventByID_EmptyID(t *testing.T) {
	client := &defaultClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://example.com",
		clientID:   "client",
		secret:     "secret",
	}

	_, err := client.GetEventByID(t.Context(), "")
	require.Error(t, err)
}

func Test_defaultClient_GetLocationsForEvent(t *testing.T) {
	t.Skip()
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_BASE_URL"), "PLANNING_CENTER_API_BASE_URL must be set to run this test")
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_CLIENT_ID"), "PLANNING_CENTER_API_CLIENT_ID must be set to run this test")
	require.NotEmpty(t, os.Getenv("PLANNING_CENTER_API_SECRET"), "PLANNING_CENTER_API_SECRET must be set to run this test")

	type fields struct {
		baseURL  string
		clientID string
		secret   string
	}
	type args struct {
		ctx                        context.Context
		eventID                    string
		includeAssociatedLocations bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []Checkout
		wantErr bool
	}{
		{
			name: "get checkins",
			args: args{
				ctx:                        t.Context(),
				eventID:                    "152112",
				includeAssociatedLocations: true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &defaultClient{
				httpClient: http.DefaultClient,
				baseURL:    os.Getenv("PLANNING_CENTER_API_BASE_URL"),
				clientID:   os.Getenv("PLANNING_CENTER_API_CLIENT_ID"),
				secret:     os.Getenv("PLANNING_CENTER_API_SECRET"),
			}
			got, err := client.GetLocationsForEvent(tt.args.ctx, tt.args.eventID)
			require.NoError(t, err)
			fmt.Printf("%+v\n", got)
		})
	}
}

func Test_defaultClient_GetEvents_EmptyNext(t *testing.T) {
	var requestCount atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"links":{"self":"%s","next":""},"data":[{"id":"event-1","type":"event","attributes":{"name":"Weekend Service"}}]}`,
			server.URL+r.URL.Path,
		)
	}))
	defer server.Close()

	client := &defaultClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
		clientID:   "client",
		secret:     "secret",
	}

	events, nextURL, err := client.GetEvents(t.Context())
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "event-1", events[0].ID)
	assert.Equal(t, "Weekend Service", events[0].Name)
	assert.Equal(t, "", nextURL)
	assert.Equal(t, int64(1), requestCount.Load())
}

func Test_defaultClient_GetEventsFromNextURL(t *testing.T) {
	var requestCount atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"links":{"self":"%s","next":"%s"},"data":[{"id":"event-2","type":"event","attributes":{"name":"Sunday Service"}}]}`,
			server.URL+r.URL.Path,
			server.URL+"/next-page",
		)
	}))
	defer server.Close()

	client := &defaultClient{
		httpClient: server.Client(),
		baseURL:    server.URL,
		clientID:   "client",
		secret:     "secret",
	}

	events, nextURL, err := client.GetEventsFromNextURL(t.Context(), server.URL+"/events")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "event-2", events[0].ID)
	assert.Equal(t, "Sunday Service", events[0].Name)
	assert.Equal(t, server.URL+"/next-page", nextURL)
	assert.Equal(t, int64(1), requestCount.Load())
}

func Test_defaultClient_GetEventsFromNextURL_EmptyURL(t *testing.T) {
	client := &defaultClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://example.com",
		clientID:   "client",
		secret:     "secret",
	}

	_, _, err := client.GetEventsFromNextURL(t.Context(), "")
	require.Error(t, err)
}

func Test_getFilteredResults(t *testing.T) {
	t1 := time.Now().Add(-10 * time.Minute)
	t2 := t1.Add(1 * time.Second)
	t3 := t2.Add(1 * time.Second)
	t4 := t3.Add(1 * time.Second)
	t5 := t4.Add(1 * time.Second)

	type args struct {
		decoded             checkinResponse
		checkedOutOnOrAfter time.Time
		limit               int
	}
	tests := []struct {
		name     string
		args     args
		wantData []Checkout
		wantDone bool
	}{
		{
			name: "empty decoded",
			args: args{
				decoded:             checkinResponse{},
				checkedOutOnOrAfter: time.Now().AddDate(10, 0, 0),
				limit:               1000,
			},
			wantData: nil,
			wantDone: false,
		},
		{
			name: "decoded",
			args: args{
				decoded: checkinResponse{
					Data: []checkinResponseData{
						{
							ID: "12345",
							Attributes: checkinAttributes{
								FirstName:    "John",
								LastName:     "Doe",
								CheckedOutAt: t5,
								SecurityCode: "ABC123",
							},
						},
						{
							ID: "12346",
							Attributes: checkinAttributes{
								FirstName:    "Mary",
								LastName:     "Sue",
								CheckedOutAt: t5,
								SecurityCode: "ABC124",
							},
						},
						{
							ID: "12347",
							Attributes: checkinAttributes{
								FirstName:    "Jerry",
								LastName:     "Jones",
								CheckedOutAt: t4,
								SecurityCode: "ABC125",
							},
						},
						{
							ID: "12348",
							Attributes: checkinAttributes{
								FirstName:    "Polly",
								LastName:     "Roger",
								CheckedOutAt: t3,
								SecurityCode: "ABC126",
							},
						},
						{
							ID: "12349",
							Attributes: checkinAttributes{
								FirstName:    "Oliver",
								LastName:     "Zamboni",
								CheckedOutAt: t3,
								SecurityCode: "ABC127",
							},
						},
						{
							ID: "12350",
							Attributes: checkinAttributes{
								FirstName:    "Steven",
								LastName:     "Hernandez",
								CheckedOutAt: t2,
								SecurityCode: "ABC128",
							},
						},
						{
							ID: "12351",
							Attributes: checkinAttributes{
								FirstName:    "Samuel",
								LastName:     "Johnson",
								CheckedOutAt: t1,
								SecurityCode: "ABC129",
							},
						},
						{
							ID: "12352",
							Attributes: checkinAttributes{
								FirstName:    "Billy",
								LastName:     "Bob",
								CheckedOutAt: t1.Add(-1 * time.Second),
								SecurityCode: "ABC130",
							},
						},
					},
				},
				checkedOutOnOrAfter: t2,
				limit:               1000,
			},
			wantData: []Checkout{
				{
					ID:           "12345",
					FirstName:    "John",
					LastName:     "Doe",
					CheckedOutAt: t5,
					SecurityCode: "ABC123",
				},
				{
					ID:           "12346",
					FirstName:    "Mary",
					LastName:     "Sue",
					CheckedOutAt: t5,
					SecurityCode: "ABC124",
				},
				{
					ID:           "12347",
					FirstName:    "Jerry",
					LastName:     "Jones",
					CheckedOutAt: t4,
					SecurityCode: "ABC125",
				},
				{
					ID:           "12348",
					FirstName:    "Polly",
					LastName:     "Roger",
					CheckedOutAt: t3,
					SecurityCode: "ABC126",
				},
				{
					ID:           "12349",
					FirstName:    "Oliver",
					LastName:     "Zamboni",
					CheckedOutAt: t3,
					SecurityCode: "ABC127",
				},
				{
					ID:           "12350",
					FirstName:    "Steven",
					LastName:     "Hernandez",
					CheckedOutAt: t2,
					SecurityCode: "ABC128",
				},
				{
					ID:           "12351",
					FirstName:    "Samuel",
					LastName:     "Johnson",
					CheckedOutAt: t1,
					SecurityCode: "ABC129",
				},
			},
			wantDone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotData, gotDone := getFilteredResults(tt.args.decoded, tt.args.checkedOutOnOrAfter, tt.args.limit)
			for i := range gotData {
				assert.False(t, gotData[i].FetchedAt.IsZero())
				assert.Equal(t, time.UTC, gotData[i].FetchedAt.Location())
				gotData[i].FetchedAt = time.Time{}
			}
			assert.Equal(t, tt.wantData, gotData)
			assert.Equal(t, tt.wantDone, gotDone)
		})
	}

}
