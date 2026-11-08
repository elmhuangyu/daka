package dbmodels

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBumpStateVersion(t *testing.T) {
	d := openTemp(t)
	g := d.Write()
	require.NoError(t, g.Create(&User{ID: "u1"}).Error)
	require.NoError(t, g.Create(&UserSettings{UserID: "u1", Timezone: "UTC", StateVersion: 5}).Error)
	require.NoError(t, g.Create(&UserSettings{UserID: "u2", Timezone: "UTC"}).Error)

	require.NoError(t, BumpStateVersion(g, "u1"))
	require.NoError(t, BumpStateVersion(g, "u1"))
	var st UserSettings
	require.NoError(t, g.Take(&st, "user_id = ?", "u1").Error)
	require.EqualValues(t, 7, st.StateVersion)
	st = UserSettings{}
	require.NoError(t, g.Take(&st, "user_id = ?", "u2").Error)
	require.EqualValues(t, 0, st.StateVersion, "other users are untouched")

	// a missing settings row is created, then bumped
	require.NoError(t, BumpStateVersion(g, "u3"))
	st = UserSettings{}
	require.NoError(t, g.Take(&st, "user_id = ?", "u3").Error)
	require.EqualValues(t, 1, st.StateVersion)
	require.Equal(t, "UTC", st.Timezone)
}

func TestBumpStateVersionConcurrent(t *testing.T) {
	d := openTemp(t)
	g := d.Write()
	require.NoError(t, g.Create(&UserSettings{UserID: "u1", Timezone: "UTC"}).Error)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assertNoErr := BumpStateVersion(g, "u1")
			if assertNoErr != nil {
				t.Error(assertNoErr)
			}
		}()
	}
	wg.Wait()
	var st UserSettings
	require.NoError(t, g.Take(&st, "user_id = ?", "u1").Error)
	require.EqualValues(t, 20, st.StateVersion)
}
