/*
 *  Copyright (c) 2018 Uber Technologies, Inc.
 *
 *     Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package assumerole_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	assumerole "github.com/uber/assume-role-cli"
	"github.com/uber/assume-role-cli/mocks"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var awsAccessDeniedError = awserr.New("AccessDenied", "Not authorized to perform sts:AssumeRole", errors.New("test"))

var fooCredentials = &assumerole.TemporaryCredentials{
	AccessKeyID:     "ABC123",
	SecretAccessKey: "supersecret",
	SessionToken:    "123tok",
	Expires:         time.Now(),
}

var fooProfileWithMFA = &assumerole.ProfileConfiguration{
	Expires:         fooCredentials.Expires,
	MFASerial:       "arn:aws:iam::000000000000:mfa/bob",
	RoleARN:         "arn:aws:iam::000000000000:role/testRole",
	RoleSessionName: "bob",
}

var fooProfileWithoutMFA = &assumerole.ProfileConfiguration{
	Expires:         fooCredentials.Expires,
	RoleARN:         "arn:aws:iam::000000000000:role/testRole-fromassumedrole",
	RoleSessionName: "bob-session",
}

type test struct {
	AssumeRoleMain *assumerole.App
	MockAWS        *mocks.MockAWSProvider
	MockAWSConfig  *mocks.MockAWSConfigProvider
	MockClock      *testClock
	MockStdin      *bytes.Buffer
	MockStderr     *bytes.Buffer
}

type testClock struct {
	time time.Time
}

func (c *testClock) Now() time.Time {
	return c.time
}

func (c *testClock) SetTime(t time.Time) {
	c.time = t
}

func newTestAssumeRole(t *testing.T, customOptions ...assumerole.Option) *test {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockAWS := mocks.NewMockAWSProvider(mockCtrl)
	mockAWSConfig := mocks.NewMockAWSConfigProvider(mockCtrl)

	mockClock := &testClock{}

	mockStdin := &bytes.Buffer{}
	mockStderr := &bytes.Buffer{}

	// Combine default test options with anything overridden for a particular
	// test
	testAssumeRoleOptions := append([]assumerole.Option{
		assumerole.WithAWS(mockAWS),
		assumerole.WithAWSConfig(mockAWSConfig),
		assumerole.WithClock(mockClock),
		assumerole.WithStdin(mockStdin),
		assumerole.WithStderr(mockStderr),
	}, customOptions...)

	main, err := assumerole.NewApp(testAssumeRoleOptions...)
	require.NoError(t, err)

	return &test{
		AssumeRoleMain: main,
		MockAWS:        mockAWS,
		MockAWSConfig:  mockAWSConfig,
		MockClock:      mockClock,
		MockStdin:      mockStdin,
		MockStderr:     mockStderr,
	}
}

func TestAssumeRoleWithMFAFirstTime(t *testing.T) {
	test := newTestAssumeRole(t)

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:iam::000000000000:user/bob", nil)
	test.MockAWS.EXPECT().Username().Return("bob", nil)
	test.MockAWS.EXPECT().MFADevices().Return([]string{fooProfileWithMFA.MFASerial}, nil)
	test.MockAWS.EXPECT().AssumeRole(fooProfileWithMFA.RoleARN, "bob").Return(nil, awsAccessDeniedError)
	test.MockAWS.EXPECT().AssumeRoleWithMFA(fooProfileWithMFA.RoleARN, "bob", fooProfileWithMFA.MFASerial, "123456").Return(fooCredentials, nil)

	test.MockAWSConfig.EXPECT().GetProfile("000000000000-testRole").Return(nil, nil)
	test.MockAWSConfig.EXPECT().SetProfile("000000000000-testRole", fooProfileWithMFA).Return(nil)
	test.MockAWSConfig.EXPECT().SetCredentials("000000000000-testRole", fooCredentials)

	test.MockStdin.WriteString("123456" + "\n")

	creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
		UserRole: fooProfileWithMFA.RoleARN,
	})
	assert.NoError(t, err)
	assert.Equal(t, fooCredentials, creds)
}

func TestErrorNoMFADevices(t *testing.T) {
	test := newTestAssumeRole(t)

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:iam::000000000000:user/bob", nil)
	test.MockAWS.EXPECT().Username().Return("bob", nil)
	test.MockAWS.EXPECT().MFADevices().Return([]string{}, nil)
	test.MockAWS.EXPECT().AssumeRole(fooProfileWithMFA.RoleARN, "bob").Return(nil, awsAccessDeniedError)
	test.MockAWS.EXPECT().AssumeRoleWithMFA(fooProfileWithMFA.RoleARN, "bob", fooProfileWithMFA.MFASerial, "123456").Return(fooCredentials, nil)

	test.MockAWSConfig.EXPECT().GetProfile("000000000000-testRole").Return(nil, nil)
	test.MockAWSConfig.EXPECT().SetProfile("000000000000-testRole", fooProfileWithMFA).Return(nil)
	test.MockAWSConfig.EXPECT().SetCredentials("000000000000-testRole", fooCredentials)

	test.MockStdin.WriteString("123456" + "\n")

	creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
		UserRole: fooProfileWithMFA.RoleARN,
	})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "error trying to AssumeRole without MFA")
	assert.Contains(t, err.Error(), "error trying to AssumeRole with MFA")
	assert.Nil(t, creds)
}

func TestMFAPromptInvalid(t *testing.T) {
	test := newTestAssumeRole(t)

	expectedCredentials := &assumerole.TemporaryCredentials{}

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:iam::000000000000:user/bob", nil)
	test.MockAWS.EXPECT().Username().Return("bob", nil)
	test.MockAWS.EXPECT().MFADevices().Return([]string{
		"foo",
		"bar",
	}, nil)
	test.MockAWS.EXPECT().AssumeRole("arn:aws:iam::000000000000:role/testRole", "bob").Return(nil, nil)
	test.MockAWS.EXPECT().AssumeRoleWithMFA("arn:aws:iam::000000000000:role/testRole", "bob", "foo", "123456").Return(expectedCredentials, nil)
	test.MockAWSConfig.EXPECT().GetProfile("000000000000-testRole").Return(nil, nil)
	test.MockAWSConfig.EXPECT().SetProfile("000000000000-testRole", gomock.Any()).Return(nil)
	test.MockAWSConfig.EXPECT().SetCredentials("000000000000-testRole", gomock.Any()).Return(nil)

	// Write responses for the prompts
	test.MockStdin.WriteString("asd\n") // invalid
	test.MockStdin.WriteString("3\n")   // invalid
	test.MockStdin.WriteString("1\n")
	test.MockStdin.WriteString("123456\n")

	creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
		UserRole: "arn:aws:iam::000000000000:role/testRole",
	})
	require.NoError(t, err)
	require.Exactly(t, expectedCredentials, creds)

	assert.Equal(t, `[1]: foo
[2]: bar
Select MFA device: Invalid input (not a number)
[1]: foo
[2]: bar
Select MFA device: Invalid input (not in range)
[1]: foo
[2]: bar
Select MFA device: Enter MFA token: `, test.MockStderr.String())
}

func TestAssumeRoleWithAssumedRoleSuccess(t *testing.T) {
	test := newTestAssumeRole(t)

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:sts::000000000000:assumed-role/testRole/bob", nil)
	test.MockAWS.EXPECT().AssumeRole(fooProfileWithoutMFA.RoleARN, "bob-session").Return(fooCredentials, nil)

	test.MockAWSConfig.EXPECT().GetProfile("000000000000-testRole-fromassumedrole").Return(nil, nil)
	test.MockAWSConfig.EXPECT().SetProfile("000000000000-testRole-fromassumedrole", fooProfileWithoutMFA).Return(nil)
	test.MockAWSConfig.EXPECT().SetCredentials("000000000000-testRole-fromassumedrole", fooCredentials)

	creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
		UserRole:        fooProfileWithoutMFA.RoleARN,
		RoleSessionName: "bob-session",
	})
	assert.NoError(t, err)
	assert.Equal(t, fooCredentials, creds)
}

func TestAssumeRoleWithAssumedRoleDoesNotTryMFA(t *testing.T) {
	test := newTestAssumeRole(t)

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:sts::000000000000:assumed-role/testRole/bob", nil)
	test.MockAWS.EXPECT().AssumeRole(fooProfileWithoutMFA.RoleARN, "bob-session").Return(nil, awsAccessDeniedError)

	test.MockAWSConfig.EXPECT().GetProfile("000000000000-testRole-fromassumedrole").Return(nil, nil)

	creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
		UserRole:        fooProfileWithoutMFA.RoleARN,
		RoleSessionName: "bob-session",
	})
	assert.Error(t, err)
	assert.Nil(t, creds)
}

func TestConfigRolePrefix(t *testing.T) {
	config, err := assumerole.LoadConfig("fixtures/test-config-roleprefix/assume-role.yaml")
	require.NoError(t, err)

	test := newTestAssumeRole(t, assumerole.WithConfig(config))

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:iam::000000000000:user/bob", nil)
	test.MockAWS.EXPECT().Username().Return("bob", nil)
	test.MockAWS.EXPECT().MFADevices().Return([]string{fooProfileWithMFA.MFASerial}, nil)
	test.MockAWS.EXPECT().AssumeRole(fooProfileWithMFA.RoleARN, "bob").Return(nil, nil)
	test.MockAWS.EXPECT().AssumeRoleWithMFA(fooProfileWithMFA.RoleARN, "bob", fooProfileWithMFA.MFASerial, "123456").Return(fooCredentials, nil)

	test.MockAWSConfig.EXPECT().GetProfile("foobar-testRole").Return(nil, nil)
	test.MockAWSConfig.EXPECT().SetProfile("foobar-testRole", fooProfileWithMFA).Return(nil)
	test.MockAWSConfig.EXPECT().SetCredentials("foobar-testRole", fooCredentials)

	test.MockStdin.WriteString("123456" + "\n")

	creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
		UserRole: "testRole",
	})
	assert.NoError(t, err)
	assert.Equal(t, fooCredentials, creds)
}

func TestCredentialsExpiry(t *testing.T) {
	mockNow := time.Date(2018, 04, 23, 23, 45, 43, 0, time.UTC)
	mockCreds := &assumerole.TemporaryCredentials{}

	config := &assumerole.Config{
		RefreshBeforeExpiry: 5 * time.Minute,
	}

	tests := []struct {
		credentialExpiry time.Time
		expectRefresh    bool
	}{
		{
			// credentials are expiring exactly now
			credentialExpiry: mockNow,
			expectRefresh:    true,
		},
		{
			// expired 1s ago
			credentialExpiry: mockNow.Add(-time.Second),
			expectRefresh:    true,
		},
		{
			// expiring in 3m
			credentialExpiry: mockNow.Add(3 * time.Minute),
			// should trigger a refresh, because it is within the refresh
			// horizon even though it's not expired yet.
			expectRefresh: true,
		},
		{
			// expiring in 10m (still valid)
			credentialExpiry: mockNow.Add(10 * time.Minute),
			expectRefresh:    false,
		},
		{
			// expired 20m ago
			credentialExpiry: mockNow.Add(-20 * time.Minute),
			expectRefresh:    true,
		},
	}

	for i, tt := range tests {
		test := newTestAssumeRole(t, assumerole.WithConfig(config))

		// Base expectations
		test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:iam::000000000000:user/bob", nil)
		test.MockAWS.EXPECT().Username().Return("bob", nil)
		test.MockAWSConfig.EXPECT().GetProfile("123-testRole").Return(&assumerole.ProfileConfiguration{
			Expires: tt.credentialExpiry,
		}, nil)
		test.MockAWSConfig.EXPECT().SetProfile("123-testRole", gomock.Any()).Return(nil)
		test.MockAWSConfig.EXPECT().SetCredentials("123-testRole", gomock.Any()).Return(nil)

		// Set mock time.Now()
		test.MockClock.SetTime(mockNow)

		if tt.expectRefresh {
			// If we're expecting a refresh, the app should call out to AWS's
			// AssumeRole, and get the credentials back.
			test.MockAWS.EXPECT().AssumeRole(gomock.Any(), gomock.Any()).Return(mockCreds, nil)
		} else {
			// If there's no refresh, there should be no AssumeRole call.
			test.MockAWS.EXPECT().AssumeRole(gomock.Any(), gomock.Any()).Do(func(roleARN string, sessionName string) {
				assert.Fail(t, fmt.Sprintf("unexpected credentials refresh; table test index: %d", i))
			})
			// Credentials should be fetched from cache.
			test.MockAWSConfig.EXPECT().GetCredentials(gomock.Any()).Return(mockCreds, nil)
		}

		creds, err := test.AssumeRoleMain.AssumeRole(assumerole.AssumeRoleParameters{
			UserRole: "arn:aws:iam::123:role/testRole",
		})
		assert.NoError(t, err)
		assert.Equal(t, mockCreds, creds)
	}
}

func TestCurrentRoleIsAssumedRole(t *testing.T) {
	test := newTestAssumeRole(t)

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:iam::000000000000:user/bob", nil)

	isAssumedRole, err := test.AssumeRoleMain.CurrentPrincipalIsAssumedRole()
	assert.NoError(t, err)
	assert.Equal(t, false, isAssumedRole)

	test.MockAWS.EXPECT().CurrentPrincipalARN().Return("arn:aws:sts::000000000000:assumed-role/testRole/bob", nil)

	isAssumedRole, err = test.AssumeRoleMain.CurrentPrincipalIsAssumedRole()
	assert.NoError(t, err)
	assert.Equal(t, true, isAssumedRole)
}
