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
package cli

import "errors"

// cliOpts are the available options for the assume-role CLI.
type cliOpts struct {
	// args is for collecting the remaidner arguments (that are not part of
	// assume-role's options). We stop parsing on first unknown option and then
	// collect the remaining args because they will be executed.
	args []string

	// role is the role name or ARN that the user wants to assume
	role string

	// roleSessionName overrides the default session name
	roleSessionName string
}

// argumentList is a special slice of strings that includes helpers for
// processing.
type argumentList []string

// used both here and in tests
var errNoRole = errors.New("Missing required argument: --role")

// Next returns the arg from the beginning of the argument list and
// removes it from the list.
func (a *argumentList) Next() string {
	s := *a

	if len(s) == 0 {
		return ""
	}

	// shift / mutate slice
	next, newList := s[0], s[1:]
	*a = newList

	return next
}

func parseOptions(args argumentList) (*cliOpts, error) {
	opts := &cliOpts{}

ArgsLoop:
	for len(args) > 0 {
		switch arg := args.Next(); arg {

		case "--role":
			opts.role = args.Next()

		case "--role-session-name":
			opts.roleSessionName = args.Next()

		case "--":
			// Stop parsing and add remaining args to opts.args
			opts.args = append(opts.args, args...)
			break ArgsLoop

		default:
			// Stop parsing and add this arg + remaining args to opts.args
			opts.args = append(opts.args, arg)
			opts.args = append(opts.args, args...)
			break ArgsLoop
		}
	}

	if opts.role == "" {
		return opts, errNoRole
	}

	return opts, nil
}
