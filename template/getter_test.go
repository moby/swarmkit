package template

import (
	"testing"

	"github.com/docker/swarmkit/agent"
	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplatedSecret(t *testing.T) {
	templatedSecret := &api.Secret{
		ID: "templatedsecret",
	}

	referencedSecret := &api.Secret{
		ID: "referencedsecret",
		Spec: api.SecretSpec{
			Data: []byte("mysecret"),
		},
	}
	referencedConfig := &api.Config{
		ID: "referencedconfig",
		Spec: api.ConfigSpec{
			Data: []byte("myconfig"),
		},
	}

	type testCase struct {
		desc        string
		secretSpec  api.SecretSpec
		task        *api.Task
		expected    string
		expectedErr string
	}

	testCases := []testCase{
		{
			desc: "Test expansion of task context",
			secretSpec: api.SecretSpec{
				Data: []byte("SERVICE_ID={{.Service.ID}}\n" +
					"SERVICE_NAME={{.Service.Name}}\n" +
					"TASK_ID={{.Task.ID}}\n" +
					"TASK_NAME={{.Task.Name}}\n" +
					"NODE_ID={{.Node.ID}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SERVICE_ID=serviceID\n" +
				"SERVICE_NAME=serviceName\n" +
				"TASK_ID=taskID\n" +
				"TASK_NAME=serviceName.10.taskID\n" +
				"NODE_ID=nodeID\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of secret, by source",
			secretSpec: api.SecretSpec{
				Data:       []byte("SECRET_VAL={{SecretBySource \"referencedsecretname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SECRET_VAL=mysecret\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
								{
									SecretID:   "referencedsecret",
									SecretName: "referencedsecretname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of secret, by target",
			secretSpec: api.SecretSpec{
				Data:       []byte("SECRET_VAL={{Secret \"referencedsecrettarget\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SECRET_VAL=mysecret\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
								{
									SecretID:   "referencedsecret",
									SecretName: "referencedsecretname",
									Target: &api.SecretReference_File{
										File: &api.FileTarget{
											Name: "referencedsecrettarget",
											UID:  "0",
											GID:  "0",
											Mode: 0666,
										},
									},
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of config, by source",
			secretSpec: api.SecretSpec{
				Data:       []byte("CONFIG_VAL={{ConfigBySource \"referencedconfigname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "CONFIG_VAL=myconfig\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "referencedconfig",
									ConfigName: "referencedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of config, by target",
			secretSpec: api.SecretSpec{
				Data:       []byte("CONFIG_VAL={{Config \"referencedconfigtarget\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "CONFIG_VAL=myconfig\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "referencedconfig",
									ConfigName: "referencedconfigname",
									Target: &api.ConfigReference_File{
										File: &api.FileTarget{
											Name: "referencedconfigtarget",
											UID:  "0",
											GID:  "0",
											Mode: 0666,
										},
									},
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of secret not available to task",
			secretSpec: api.SecretSpec{
				Data:       []byte("SECRET_VAL={{SecretBySource \"referencedsecretname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expectedErr: `failed to expand templated secret templatedsecret: template: expansion:1:13: executing "expansion" at <SecretBySource "refe...>: error calling SecretBySource: secret source referencedsecretname not found`,
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of config not available to task",
			secretSpec: api.SecretSpec{
				Data:       []byte("CONFIG_VAL={{ConfigBySource \"referencedconfigname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expectedErr: `failed to expand templated secret templatedsecret: template: expansion:1:13: executing "expansion" at <ConfigBySource "refe...>: error calling ConfigBySource: config source referencedconfigname not found`,
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test that expansion of the same secret avoids recursion",
			secretSpec: api.SecretSpec{
				Data:       []byte("SECRET_VAL={{SecretBySource \"templatedsecretname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SECRET_VAL=SECRET_VAL={{SecretBySource \"templatedsecretname\"}}\n\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test env",
			secretSpec: api.SecretSpec{
				Data: []byte("ENV VALUE={{Env \"foo\"}}\n" +
					"DOES NOT EXIST={{Env \"badname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "ENV VALUE=bar\n" +
				"DOES NOT EXIST=\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "templatedsecret",
									SecretName: "templatedsecretname",
								},
							},
							Env: []string{"foo=bar"},
						},
					},
				}
			}),
		},
	}

	for _, testCase := range testCases {
		templatedSecret.Spec = testCase.secretSpec

		dependencyManager := agent.NewDependencyManager()
		dependencyManager.Secrets().Add(*templatedSecret, *referencedSecret)
		dependencyManager.Configs().Add(*referencedConfig)

		templatedDependencies := NewTemplatedDependencyGetter(agent.Restrict(dependencyManager, testCase.task), testCase.task)
		expandedSecret, err := templatedDependencies.Secrets().Get("templatedsecret")

		if testCase.expectedErr != "" {
			assert.EqualError(t, err, testCase.expectedErr)
		} else {
			assert.NoError(t, err)
			require.NotNil(t, expandedSecret)
			assert.Equal(t, testCase.expected, string(expandedSecret.Spec.Data), testCase.desc)
		}
	}
}

func TestTemplatedConfig(t *testing.T) {
	templatedConfig := &api.Config{
		ID: "templatedconfig",
	}

	referencedSecret := &api.Secret{
		ID: "referencedsecret",
		Spec: api.SecretSpec{
			Data: []byte("mysecret"),
		},
	}
	referencedConfig := &api.Config{
		ID: "referencedconfig",
		Spec: api.ConfigSpec{
			Data: []byte("myconfig"),
		},
	}

	type testCase struct {
		desc        string
		configSpec  api.ConfigSpec
		task        *api.Task
		expected    string
		expectedErr string
	}

	testCases := []testCase{
		{
			desc: "Test expansion of task context",
			configSpec: api.ConfigSpec{
				Data: []byte("SERVICE_ID={{.Service.ID}}\n" +
					"SERVICE_NAME={{.Service.Name}}\n" +
					"TASK_ID={{.Task.ID}}\n" +
					"TASK_NAME={{.Task.Name}}\n" +
					"NODE_ID={{.Node.ID}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SERVICE_ID=serviceID\n" +
				"SERVICE_NAME=serviceName\n" +
				"TASK_ID=taskID\n" +
				"TASK_NAME=serviceName.10.taskID\n" +
				"NODE_ID=nodeID\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of secret, by source",
			configSpec: api.ConfigSpec{
				Data:       []byte("SECRET_VAL={{SecretBySource \"referencedsecretname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SECRET_VAL=mysecret\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "referencedsecret",
									SecretName: "referencedsecretname",
								},
							},
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of secret, by target",
			configSpec: api.ConfigSpec{
				Data:       []byte("SECRET_VAL={{Secret \"referencedsecrettarget\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "SECRET_VAL=mysecret\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Secrets: []*api.SecretReference{
								{
									SecretID:   "referencedsecret",
									SecretName: "referencedsecretname",
									Target: &api.SecretReference_File{
										File: &api.FileTarget{
											Name: "referencedsecrettarget",
											UID:  "0",
											GID:  "0",
											Mode: 0666,
										},
									},
								},
							},
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of config, by source",
			configSpec: api.ConfigSpec{
				Data:       []byte("CONFIG_VAL={{ConfigBySource \"referencedconfigname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "CONFIG_VAL=myconfig\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
								{
									ConfigID:   "referencedconfig",
									ConfigName: "referencedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of config, by target",
			configSpec: api.ConfigSpec{
				Data:       []byte("CONFIG_VAL={{Config \"referencedconfigtarget\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "CONFIG_VAL=myconfig\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
								{
									ConfigID:   "referencedconfig",
									ConfigName: "referencedconfigname",
									Target: &api.ConfigReference_File{
										File: &api.FileTarget{
											Name: "referencedconfigtarget",
											UID:  "0",
											GID:  "0",
											Mode: 0666,
										},
									},
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of secret not available to task",
			configSpec: api.ConfigSpec{
				Data:       []byte("SECRET_VAL={{SecretBySource \"referencedsecretname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expectedErr: `failed to expand templated config templatedconfig: template: expansion:1:13: executing "expansion" at <SecretBySource "refe...>: error calling SecretBySource: secret source referencedsecretname not found`,
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test expansion of config not available to task",
			configSpec: api.ConfigSpec{
				Data:       []byte("CONFIG_VAL={{ConfigBySource \"referencedconfigname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expectedErr: `failed to expand templated config templatedconfig: template: expansion:1:13: executing "expansion" at <ConfigBySource "refe...>: error calling ConfigBySource: config source referencedconfigname not found`,
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test that expansion of the same config avoids recursion",
			configSpec: api.ConfigSpec{
				Data:       []byte("CONFIG_VAL={{ConfigBySource \"templatedconfigname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "CONFIG_VAL=CONFIG_VAL={{ConfigBySource \"templatedconfigname\"}}\n\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
						},
					},
				}
			}),
		},
		{
			desc: "Test env",
			configSpec: api.ConfigSpec{
				Data: []byte("ENV VALUE={{Env \"foo\"}}\n" +
					"DOES NOT EXIST={{Env \"badname\"}}\n"),
				Templating: api.Templating_GO_TEMPLATE,
			},
			expected: "ENV VALUE=bar\n" +
				"DOES NOT EXIST=\n",
			task: modifyTask(func(t *api.Task) {
				t.Spec = api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Configs: []*api.ConfigReference{
								{
									ConfigID:   "templatedconfig",
									ConfigName: "templatedconfigname",
								},
							},
							Env: []string{"foo=bar"},
						},
					},
				}
			}),
		},
	}

	for _, testCase := range testCases {
		templatedConfig.Spec = testCase.configSpec

		dependencyManager := agent.NewDependencyManager()
		dependencyManager.Configs().Add(*templatedConfig, *referencedConfig)
		dependencyManager.Secrets().Add(*referencedSecret)

		templatedDependencies := NewTemplatedDependencyGetter(agent.Restrict(dependencyManager, testCase.task), testCase.task)
		expandedConfig, err := templatedDependencies.Configs().Get("templatedconfig")

		if testCase.expectedErr != "" {
			assert.EqualError(t, err, testCase.expectedErr)
		} else {
			assert.NoError(t, err)
			require.NotNil(t, expandedConfig)
			assert.Equal(t, testCase.expected, string(expandedConfig.Spec.Data), testCase.desc)
		}
	}
}
