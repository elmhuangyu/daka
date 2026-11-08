package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func TestHolidayCalendars(t *testing.T) {
	hp, err := newHolidayProvider(config.HolidayConfig{})
	require.NoError(t, err)
	require.NotNil(t, hp)
	f := holidayCalendars(hp)
	cn := f(dbmodels.RegionCN)
	require.NotNil(t, cn)
	require.True(t, cn.IsStatutoryHoliday("2025-10-01"))
	require.Nil(t, f(dbmodels.RegionUS), "region without data: no calendar")
	require.Nil(t, f(""), "unset region: no calendar")

	hp, err = newHolidayProvider(config.HolidayConfig{Disabled: true})
	require.NoError(t, err)
	require.Nil(t, hp)
	require.Nil(t, holidayCalendars(nil))

	_, err = newHolidayProvider(config.HolidayConfig{URLTemplate: "https://example.com/{year}.json"})
	require.NoError(t, err)
}
