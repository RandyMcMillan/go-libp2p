package libp2pwebtransport

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/test"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

func certificateHashFromTLSConfig(c *tls.Config) [32]byte {
	return sha256.Sum256(c.Certificates[0].Certificate[0])
}

func splitMultiaddr(addr ma.Multiaddr) []ma.Component {
	var components []ma.Component
	ma.ForEach(addr, func(c ma.Component) bool {
		components = append(components, c)
		return true
	})
	return components
}

func certHashFromComponent(t *testing.T, comp ma.Component) []byte {
	t.Helper()
	_, data, err := multibase.Decode(comp.Value())
	require.NoError(t, err)
	mh, err := multihash.Decode(data)
	require.NoError(t, err)
	require.Equal(t, uint64(multihash.SHA2_256), mh.Code)
	return mh.Digest
}

func TestInitialCert(t *testing.T) {
	cl := clock.NewMock()
	cl.Add(1234567 * time.Hour)
	priv, _, err := test.RandTestKeyPair(crypto.Ed25519, 32)
	require.NoError(t, err)
	m, err := newCertManager(priv, cl)
	require.NoError(t, err)
	defer m.Close()

	conf := m.GetConfig()
	require.Len(t, conf.Certificates, 1)
	cert := conf.Certificates[0]
	timeBuckets := getTimeBuckets(cl.Now())
	require.Equal(t, timeBuckets.current.UTC(), cert.Leaf.NotBefore)
	require.Equal(t, cert.Leaf.NotBefore.Add(certValidity), cert.Leaf.NotAfter)
	addr := m.AddrComponent()
	components := splitMultiaddr(addr)
	require.Len(t, components, 2)
	require.Equal(t, ma.P_CERTHASH, components[0].Protocol().Code)
	hash := certificateHashFromTLSConfig(conf)
	require.Equal(t, hash[:], certHashFromComponent(t, components[0]))
	require.Equal(t, ma.P_CERTHASH, components[1].Protocol().Code)
}

func TestCertRenewal(t *testing.T) {
	cl := clock.NewMock()
	cl.Set(time.UnixMilli(0))
	priv, _, err := test.RandTestKeyPair(crypto.Ed25519, 32)
	require.NoError(t, err)
	m, err := newCertManager(priv, cl)
	require.NoError(t, err)
	defer m.Close()

	firstConf := m.GetConfig()
	first := splitMultiaddr(m.AddrComponent())
	require.Len(t, first, 2)
	require.NotEqual(t, first[0].Value(), first[1].Value(), "the hashes should differ")
	// wait for a new certificate to be generated
	cl.Add(certValidity - 2*clockSkewAllowance - time.Second)
	require.Never(t, func() bool {
		for i, c := range splitMultiaddr(m.AddrComponent()) {
			if c.Value() != first[i].Value() {
				return true
			}
		}
		return false
	}, 100*time.Millisecond, 10*time.Millisecond)
	cl.Add(clockSkewAllowance + 2*time.Second)
	require.Eventually(t, func() bool { return m.GetConfig() != firstConf }, 200*time.Millisecond, 10*time.Millisecond)
	secondConf := m.GetConfig()

	second := splitMultiaddr(m.AddrComponent())
	require.Len(t, second, 2)
	for _, c := range second {
		require.Equal(t, ma.P_CERTHASH, c.Protocol().Code)
	}
	// check that the 2nd certificate from the beginning was rolled over to be the 1st certificate
	require.Equal(t, first[1].Value(), second[0].Value())
	require.NotEqual(t, first[0].Value(), second[1].Value())

	cl.Add(certValidity - 2*clockSkewAllowance + time.Second)
	require.Eventually(t, func() bool { return m.GetConfig() != secondConf }, 200*time.Millisecond, 10*time.Millisecond)
	third := splitMultiaddr(m.AddrComponent())
	require.Len(t, third, 2)
	for _, c := range third {
		require.Equal(t, ma.P_CERTHASH, c.Protocol().Code)
	}
	// check that the 2nd certificate from the beginning was rolled over to be the 1st certificate
	require.Equal(t, second[1].Value(), third[0].Value())
}

func TestDeterministicCertsAcrossReboots(t *testing.T) {
	// Run this test 100 times to make sure it's deterministic
	runs := 100
	for i := 0; i < runs; i++ {
		t.Run(fmt.Sprintf("Run=%d", i), func(t *testing.T) {
			cl := clock.NewMock()
			priv, _, err := test.RandTestKeyPair(crypto.Ed25519, 32)
			require.NoError(t, err)
			m, err := newCertManager(priv, cl)
			require.NoError(t, err)
			defer m.Close()

			conf := m.GetConfig()
			require.Len(t, conf.Certificates, 1)
			oldCerts := m.serializedCertHashes

			m.Close()

			cl.Add(time.Hour)
			// reboot
			m, err = newCertManager(priv, cl)
			require.NoError(t, err)
			defer m.Close()

			newCerts := m.serializedCertHashes

			require.Equal(t, oldCerts, newCerts)
		})
	}
}

func TestDeterministicTimeBuckets(t *testing.T) {
	bucketsA := getTimeBuckets((time.Time{}))
	bucketsB := getTimeBuckets((time.Time{}.Add(time.Hour * 24)))
	require.Equal(t, bucketsA.current, bucketsB.current)
	require.Equal(t, bucketsA.next, bucketsB.next)

	// 15 Days later
	bucketsC := getTimeBuckets((time.Time{}.Add(time.Hour * 24 * 15)))
	require.Equal(t, bucketsC.current, bucketsB.next)
	require.NotEqual(t, bucketsC.next, bucketsB.next)
	require.NotEqual(t, bucketsC.current, bucketsB.current)
}
