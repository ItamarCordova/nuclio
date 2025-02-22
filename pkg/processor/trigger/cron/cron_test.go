//go:build test_unit

/*
Copyright 2017 The Nuclio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cron

import (
	"fmt"
	"testing"
	"time"

	"github.com/nuclio/errors"
	"github.com/nuclio/logger"
	nucliozap "github.com/nuclio/zap"
	cronlib "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
	trigger cron
	logger  logger.Logger
}

func (suite *TestSuite) SetupSuite() {
	suite.logger, _ = nucliozap.NewNuclioZapTest("test")
}

func (suite *TestSuite) SetupTest() {
	suite.trigger = cron{}
	suite.trigger.Logger = suite.logger.GetChild("cron")
}

func (suite *TestSuite) TestScheduleBackwardsCompatibility() {
	schedule, err := suite.trigger.parseEncodedSchedule("* */5 * * * *")
	suite.Require().NoError(err)
	scheduler := schedule.(*cronlib.SpecSchedule)
	suite.Require().Equal(uint64(1), scheduler.Second) // minimal value for non-set value is 1
}

func (suite *TestSuite) TestGetInterval() {
	var err error

	suite.trigger.tickMethod = tickMethodInterval

	tests := []struct {
		delayInterval      string
		lastTimeDifference time.Duration
	}{
		// no misses
		{"5ms", 0},
		{"250ms", 0},
		{"5s", 0},
		{"5m", 0},
		{"5h", 0},

		// misses
		{"1ms", time.Millisecond},
		{"1ms", 150 * time.Millisecond},
		{"250ms", time.Second},
		{"1s", time.Second},
		{"1s", time.Minute},
		{"1m", time.Minute},
		{"1m", time.Hour},
		{"1h", time.Hour},
		{"1h", 24 * time.Hour},
	}

	for _, test := range tests {
		suite.trigger.schedule, err = suite.getInterval(test.delayInterval)
		suite.Require().NoError(err, "Invalid interval string")
		delay := suite.trigger.schedule.(cronlib.ConstantDelaySchedule).Delay

		// test delay
		lastRuntime := time.Now().Add(-test.lastTimeDifference)
		nextEventDelay := suite.trigger.getNextEventSubmitDelay(suite.trigger.schedule, lastRuntime)

		suite.Require().Conditionf(func() (success bool) {
			return nextEventDelay <= delay
		}, "Next event delay must be less or equal to interval's delay")

		// test misses ticks
		lastRuntime = time.Now().Add(-test.lastTimeDifference)
		missedTicks := suite.trigger.getMissedTicks(suite.trigger.schedule, lastRuntime)
		expectedMissedTicks := int(test.lastTimeDifference / delay)
		suite.Require().EqualValues(expectedMissedTicks, missedTicks)
	}
}

func (suite *TestSuite) TestGetMissedTicksScheduleHandlesNoMisses() {
	var err error
	suite.trigger.schedule, err = suite.trigger.parseEncodedSchedule("*/5 * * * *")
	suite.Assert().NoError(err, "Invalid interval string")

	lastRuntime := time.Now()
	missedTicks := suite.trigger.getMissedTicks(suite.trigger.schedule, lastRuntime)

	suite.Assert().EqualValues(0, missedTicks)
}

func (suite *TestSuite) TestGetMissedTicksScheduleCountsMisses() {
	var err error
	suite.trigger.schedule, err = suite.trigger.parseEncodedSchedule("*/5 * * * * *")
	suite.Assert().NoError(err, "Invalid interval string")

	lastTimeDifference, err := time.ParseDuration("10s")
	suite.Require().NoError(err)

	lastRuntime := time.Now().Add(-lastTimeDifference)
	missedTicks := suite.trigger.getMissedTicks(suite.trigger.schedule, lastRuntime)

	suite.Assert().EqualValues(2, missedTicks)
}

func (suite *TestSuite) TestGetNextEventSubmitDelayScheduleNoMisses() {
	var err error

	suite.trigger.schedule, err = suite.trigger.parseEncodedSchedule("*/5 * * * *")
	suite.Assert().NoError(err, "Invalid interval string")

	lastRuntime := time.Now()
	nextEventDelay := suite.trigger.getNextEventSubmitDelay(suite.trigger.schedule, lastRuntime)

	expectedEventDelay, err := time.ParseDuration("5m")
	suite.Assert().NoError(err, "Invalid interval string")

	suite.Assert().Condition(
		func() bool { return nextEventDelay > 0 && nextEventDelay < expectedEventDelay },
		"Expected delay between 0 and %s",
		expectedEventDelay,
		nextEventDelay,
	)
}

func (suite *TestSuite) TestGetNextEventSubmitDelayScheduleRunsImmediatelyOnMiss() {
	var err error

	suite.trigger.schedule, err = suite.trigger.parseEncodedSchedule("*/5 * * * *")
	suite.Assert().NoError(err, "Invalid interval string")

	lastTimeDifference, err := time.ParseDuration("10m")
	suite.Require().NoError(err)

	lastRuntime := time.Now().Add(-lastTimeDifference)
	nextEventDelay := suite.trigger.getNextEventSubmitDelay(suite.trigger.schedule, lastRuntime)

	suite.Assert().EqualValues(0, nextEventDelay)
}

func (suite *TestSuite) TestNextScheduleDayDifference() {
	var err error

	// mock runtime
	location, _ := time.LoadLocation("UTC")
	lastRuntime := time.Date(2019, 1, 1, 1, 1, 1, 1, location)

	scheduleFormat := fmt.Sprintf("%d %d * * *", lastRuntime.Minute(), lastRuntime.Hour())

	suite.trigger.schedule, err = suite.trigger.parseEncodedSchedule(scheduleFormat)
	suite.Require().NoError(err, "Invalid interval string")

	nextEventSubmitTime := suite.trigger.schedule.Next(lastRuntime)
	suite.Require().Equal(nextEventSubmitTime.Day(), lastRuntime.Day()+1, "Event should be fired the next day")
}

func (suite *TestSuite) getInterval(delay string) (cronlib.Schedule, error) {
	delayDuration, err := time.ParseDuration(delay)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse duration string %s", delay)
	}

	return cronlib.ConstantDelaySchedule{Delay: delayDuration}, nil
}

func TestCronSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}
