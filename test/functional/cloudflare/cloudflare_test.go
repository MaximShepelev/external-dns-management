// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package cloudflare_test

import (
	"os"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/util/flowcontrol"

	cfprovider "github.com/gardener/external-dns-management/pkg/controller/provider/cloudflare"
	"github.com/gardener/external-dns-management/pkg/dns/provider"
)

const (
	testRecord = "test-record"
)

func TestCloudflareAccess(t *testing.T) {
	// Skip if not running in CI or explicitly enabled
	if os.Getenv("TEST_CLOUDFLARE") != "true" {
		t.Skip("Skipping Cloudflare integration test. Set TEST_CLOUDFLARE=true to run.")
	}

	// Get required environment variables
	apiToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	if apiToken == "" {
		t.Fatal("CLOUDFLARE_API_TOKEN environment variable is required")
	}

	zoneName := os.Getenv("CLOUDFLARE_ZONE_NAME")
	if zoneName == "" {
		t.Fatal("CLOUDFLARE_ZONE_NAME environment variable is required")
	}

	domain := os.Getenv("CLOUDFLARE_DOMAIN")
	if domain == "" {
		t.Fatal("CLOUDFLARE_DOMAIN environment variable is required")
	}

	// Initialize Cloudflare access
	metrics := &provider.NullMetrics{}
	rateLimiter := flowcontrol.NewTokenBucketRateLimiter(1, 1)
	access, err := cfprovider.NewAccess(apiToken, metrics, rateLimiter)
	require.NoError(t, err)
	require.NotNil(t, access)

	// Find zone ID
	t.Log("Looking up zone ID for", zoneName)
	var zoneID string
	err = access.ListZones(func(z cloudflare.Zone) (bool, error) {
		if z.Name == zoneName {
			zoneID = z.ID
			t.Logf("Found zone: ID=%s, Name=%s", z.ID, z.Name)
			return false, nil
		}
		return true, nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, zoneID, "Zone not found")

	// Create test zone reference
	testZone := provider.NewDNSHostedZone("cloudflare", zoneID, domain, zoneID, false)

	// Test ListZones
	t.Run("ListZones", func(t *testing.T) {
		var found bool
		err := access.ListZones(func(z cloudflare.Zone) (bool, error) {
			if z.ID == zoneID {
				found = true
			}
			return true, nil
		})
		require.NoError(t, err)
		assert.True(t, found, "Test zone should be found in ListZones")
	})

	// Test CreateRecord
	t.Run("CreateRecord", func(t *testing.T) {
		record := access.NewRecord(testRecord+"."+domain, "A", "1.2.3.4", testZone, 300)
		t.Logf("Creating record: Name=%s, Type=%s, Content=%s, TTL=%d",
			record.GetDNSName(), record.GetType(), record.GetValue(), record.GetTTL())
		err := access.CreateRecord(record, testZone)
		require.NoError(t, err)
		t.Log("Record created successfully")
	})

	// Test ListRecords
	t.Run("ListRecords", func(t *testing.T) {
		var foundRecord *cloudflare.DNSRecord
		t.Log("Listing records to verify creation")
		err := access.ListRecords(zoneID, func(r cloudflare.DNSRecord) (bool, error) {
			t.Logf("Found record: ID=%s, Name=%s, Type=%s, Content=%s, TTL=%d",
				r.ID, r.Name, r.Type, r.Content, r.TTL)
			if r.Name == testRecord+"."+domain && r.Type == "A" {
				foundRecord = &r
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err)
		require.NotNil(t, foundRecord, "Test record not found")
		t.Logf("Successfully found test record: ID=%s", foundRecord.ID)
	})

	// Test GetRecordSet
	t.Run("GetRecordSet", func(t *testing.T) {
		t.Log("Getting record set for", testRecord+"."+domain)
		rs, err := access.GetRecordSet(testRecord+"."+domain, "A", testZone)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		record := rs[0]
		t.Logf("Retrieved record: Name=%s, Type=%s, Content=%s, TTL=%d, ID=%s",
			record.GetDNSName(), record.GetType(), record.GetValue(), record.GetTTL(), record.GetId())
		assert.Equal(t, "1.2.3.4", record.GetValue())
	})

	// Test UpdateRecord
	t.Run("UpdateRecord", func(t *testing.T) {
		rs, err := access.GetRecordSet(testRecord+"."+domain, "A", testZone)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		record := rs[0]

		// Update TTL
		t.Logf("Updating record ID=%s: changing TTL from %d to 600", record.GetId(), record.GetTTL())
		record.SetTTL(600)
		err = access.UpdateRecord(record, testZone)
		require.NoError(t, err)
		t.Log("Record updated successfully")

		// Verify update
		t.Log("Verifying record update")
		rs, err = access.GetRecordSet(testRecord+"."+domain, "A", testZone)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		updatedRecord := rs[0]
		t.Logf("Retrieved updated record: TTL=%d", updatedRecord.GetTTL())
		assert.Equal(t, int64(600), updatedRecord.GetTTL())
	})

	// Test DeleteRecord
	t.Run("DeleteRecord", func(t *testing.T) {
		rs, err := access.GetRecordSet(testRecord+"."+domain, "A", testZone)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		record := rs[0]

		t.Logf("Deleting record: ID=%s", record.GetId())
		err = access.DeleteRecord(record, testZone)
		require.NoError(t, err)
		t.Log("Record deleted successfully")

		// Verify deletion
		t.Log("Verifying record deletion")
		rs, err = access.GetRecordSet(testRecord+"."+domain, "A", testZone)
		require.NoError(t, err)
		assert.Len(t, rs, 0, "Record should be deleted")
		t.Log("Verified record was deleted")
	})
}
