package tribe

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/pborman/uuid"

	. "github.com/smartystreets/goconvey/convey"
)

func init() {
	log.SetLevel(log.InfoLevel)
}

func TestFullStateSyncOnJoin(t *testing.T) {
	numOfTribes := 2
	agreement1 := "agreement1"
	plugin1 := plugin{Name: "plugin1", Version: 1}
	plugin2 := plugin{Name: "plugin2", Version: 1}
	task1 := task{ID: uuid.New()}
	task2 := task{ID: uuid.New()}
	Convey("A seed is started", t, func() {
		conf := DefaultConfig("seed", "127.0.0.1", 8599, "")
		seed, err := New(conf)
		So(err, ShouldBeNil)
		So(seed, ShouldNotBeNil)
		Convey("agreements are added", func() {
			seed.AddAgreement(agreement1)
			seed.JoinAgreement(agreement1, "seed")
			seed.AddPlugin(agreement1, plugin1.Name, plugin1.Version)
			seed.AddPlugin(agreement1, plugin2.Name, plugin2.Version)
			seed.AddTask(agreement1, task1.ID)
			seed.AddTask(agreement1, task2.ID)
			So(seed.intentBuffer, ShouldBeEmpty)
			So(len(seed.members["seed"].PluginAgreement.Plugins), ShouldEqual, 2)
			So(len(seed.members["seed"].TaskAgreements[agreement1].Tasks), ShouldEqual, 2)
			Convey("members are added", func() {
				tribes := getTribes(numOfTribes, 8500, seed)
				for i := 0; i < numOfTribes; i++ {
					log.Debugf("%v is reporting %v members", i, len(tribes[i].memberlist.Members()))
					So(len(tribes[i].memberlist.Members()), ShouldEqual, numOfTribes)
				}
				Convey("members agree on tasks and plugins", func() {
					for _, tr := range tribes {
						So(len(tr.agreements), ShouldEqual, 1)
						So(len(tr.agreements[agreement1].PluginAgreement.Plugins), ShouldEqual, 2)
						So(len(tr.agreements[agreement1].TaskAgreement.Tasks), ShouldEqual, 2)
						So(len(tr.members[seed.memberlist.LocalNode().Name].PluginAgreement.Plugins), ShouldEqual, 2)
						So(len(tr.members[seed.memberlist.LocalNode().Name].TaskAgreements[agreement1].Tasks), ShouldEqual, 2)
					}
					Convey("new members join agreement", func() {
						for _, tr := range tribes {
							if tr.memberlist.LocalNode().Name == "seed" {
								continue
							}
							tr.JoinAgreement(agreement1, tr.memberlist.LocalNode().Name)
						}
						Convey("members agree on tasks, plugins and membership", func() {
							var wg sync.WaitGroup
							for _, tr := range tribes {
								wg.Add(1)
								go func(tr *tribe) {
									defer wg.Done()
									if _, ok := tr.members[tr.memberlist.LocalNode().Name]; ok {
										if tr.members[tr.memberlist.LocalNode().Name].PluginAgreement != nil {
											return
										}
									}
								}(tr)
							}
							wg.Wait()
							for _, tr := range tribes {
								So(len(tr.agreements), ShouldEqual, 1)
								So(len(tr.agreements[agreement1].PluginAgreement.Plugins), ShouldEqual, 2)
								So(len(tr.agreements[agreement1].TaskAgreement.Tasks), ShouldEqual, 2)
								So(len(tr.members[seed.memberlist.LocalNode().Name].PluginAgreement.Plugins), ShouldEqual, 2)
								So(len(tr.members[seed.memberlist.LocalNode().Name].TaskAgreements[agreement1].Tasks), ShouldEqual, 2)
							}
						})
					})
				})
			})
		})
	})
}

func TestTaskAgreements(t *testing.T) {
	numOfTribes := 10
	tribes := getTribes(numOfTribes, 8400, nil)
	Convey(fmt.Sprintf("%d tribes are started", numOfTribes), t, func() {
		for i := 0; i < numOfTribes; i++ {
			log.Debugf("%v is reporting %v members", i, len(tribes[i].memberlist.Members()))
			So(len(tribes[0].memberlist.Members()), ShouldEqual, len(tribes[i].memberlist.Members()))
		}
		Convey("the cluster agrees on membership", func() {
			for i := 0; i < numOfTribes; i++ {
				So(
					len(tribes[0].memberlist.Members()),
					ShouldEqual,
					len(tribes[i].memberlist.Members()),
				)
				So(len(tribes[0].members), ShouldEqual, len(tribes[i].members))
			}

			agreementName := "agreement1"
			agreementName2 := "agreement2"
			task1 := task{ID: uuid.New()}
			task2 := task{ID: uuid.New()}
			Convey("a member handles", func() {
				t := tribes[rand.Intn(numOfTribes)]
				Convey("an out of order 'add task' message", func() {
					msg := &taskMsg{
						LTime:         t.clock.Increment(),
						UUID:          uuid.New(),
						TaskID:        task1.ID,
						AgreementName: agreementName,
						Type:          addTaskMsgType,
					}
					b := t.handleAddTask(msg)
					So(b, ShouldEqual, true)
					t.broadcast(addTaskMsgType, msg, nil)
					So(len(t.intentBuffer), ShouldEqual, 1)
					err := t.AddTask(agreementName, task1.ID)
					So(err.Error(), ShouldResemble, errAgreementDoesNotExist.Error())
					err = t.AddAgreement(agreementName)
					So(err, ShouldBeNil)
					So(len(t.intentBuffer), ShouldEqual, 0)
					So(len(t.agreements[agreementName].TaskAgreement.Tasks), ShouldEqual, 1)
					ok, _ := t.agreements[agreementName].TaskAgreement.Tasks.contains(task1)
					So(ok, ShouldBeTrue)
					Convey("adding an existing task", func() {
						err := t.AddTask(agreementName, task1.ID)
						So(err.Error(), ShouldResemble, errTaskAlreadyExists.Error())
						Convey("removing a task that doesn't exist", func() {
							err := t.RemoveTask(agreementName, "1234")
							So(err.Error(), ShouldResemble, errTaskDoesNotExist.Error())
							err = t.RemoveTask("doesn't exist", task1.ID)
							So(err.Error(), ShouldResemble, errAgreementDoesNotExist.Error())
							Convey("joining an agreement with tasks", func() {
								err := t.AddAgreement(agreementName2)
								So(err, ShouldBeNil)
								err = t.AddTask(agreementName2, task2.ID)
								So(err, ShouldBeNil)
								err = t.JoinAgreement(agreementName, t.memberlist.LocalNode().Name)
								So(err, ShouldBeNil)
								err = t.JoinAgreement(agreementName2, t.memberlist.LocalNode().Name)
								So(err, ShouldBeNil)
								So(len(t.members[t.memberlist.LocalNode().Name].TaskAgreements), ShouldEqual, 2)
								err = t.canJoinAgreement(agreementName2, t.memberlist.LocalNode().Name)
								So(err, ShouldBeNil)
								So(t.members[t.memberlist.LocalNode().Name].PluginAgreement, ShouldNotBeNil)
								So(len(t.members[t.memberlist.LocalNode().Name].TaskAgreements), ShouldEqual, 2)
								Convey("all members agree on tasks", func(c C) {
									var wg sync.WaitGroup
									for _, t := range tribes {
										wg.Add(1)
										go func(t *tribe) {
											defer wg.Done()
											for {
												if a, ok := t.agreements[agreementName]; ok {
													if ok, _ := a.TaskAgreement.Tasks.contains(task1); ok {
														return
													}
												}
												time.Sleep(50 * time.Millisecond)
											}
										}(t)
									}
									wg.Wait()
									Convey("a member handles removing a task", func() {
										t := tribes[rand.Intn(numOfTribes)]
										So(t.intentBuffer, ShouldBeEmpty)
										ok, _ := t.agreements[agreementName].TaskAgreement.Tasks.contains(task1)
										So(ok, ShouldBeTrue)
										err := t.RemoveTask(agreementName, task1.ID)
										ok, _ = t.agreements[agreementName].TaskAgreement.Tasks.contains(task1)
										So(ok, ShouldBeFalse)
										So(err, ShouldBeNil)
										So(t.intentBuffer, ShouldBeEmpty)
										var wg sync.WaitGroup
										for _, t := range tribes {
											wg.Add(1)
											go func(t *tribe) {
												defer wg.Done()
												for {
													if a, ok := t.agreements[agreementName]; ok {
														if len(a.TaskAgreement.Tasks) == 0 {
															return
														}
													}
													time.Sleep(50 * time.Millisecond)
												}
											}(t)
										}
										wg.Wait()
									})
								})
							})
						})
					})
				})
			})
		})
	})
}

func TestTribeAgreements(t *testing.T) {
	numOfTribes := 10
	tribes := getTribes(numOfTribes, 8300, nil)
	Convey(fmt.Sprintf("%d tribes are started", numOfTribes), t, func() {
		for i := 0; i < numOfTribes; i++ {
			log.Debugf("%v is reporting %v members", i, len(tribes[i].memberlist.Members()))
			So(len(tribes[0].memberlist.Members()), ShouldEqual, len(tribes[i].memberlist.Members()))
		}
		Convey("The cluster agrees on membership", func() {
			for i := 0; i < numOfTribes; i++ {

				So(
					len(tribes[0].memberlist.Members()),
					ShouldEqual,
					len(tribes[i].memberlist.Members()),
				)
				So(len(tribes[0].members), ShouldEqual, len(tribes[i].members))
			}

			Convey("A member handles", func() {
				agreementName := "agreement1"
				t := tribes[rand.Intn(numOfTribes)]
				Convey("an out-of-order join agreement message", func() {
					msg := &agreementMsg{
						LTime:         t.clock.Increment(),
						UUID:          uuid.New(),
						AgreementName: agreementName,
						MemberName:    t.memberlist.LocalNode().Name,
						Type:          joinAgreementMsgType,
					}

					b := t.handleJoinAgreement(msg)
					So(b, ShouldEqual, true)
					So(len(t.intentBuffer), ShouldEqual, 1)
					t.broadcast(joinAgreementMsgType, msg, nil)

					Convey("an out-of-order add plugin message", func() {
						plugin := plugin{Name: "plugin1", Version: 1}
						msg := &pluginMsg{
							LTime:         t.clock.Increment(),
							UUID:          uuid.New(),
							Plugin:        plugin,
							AgreementName: agreementName,
							Type:          addPluginMsgType,
						}
						b := t.handleAddPlugin(msg)
						So(b, ShouldEqual, true)
						So(len(t.intentBuffer), ShouldEqual, 2)
						t.broadcast(addPluginMsgType, msg, nil)

						Convey("an add agreement", func() {
							err := t.AddAgreement(agreementName)
							So(err, ShouldBeNil)
							err = t.AddAgreement(agreementName)
							So(err.Error(), ShouldResemble, errAgreementAlreadyExists.Error())
							var wg sync.WaitGroup
							for _, t := range tribes {
								wg.Add(1)
								go func(t *tribe) {
									defer wg.Done()
									for {
										if a, ok := t.agreements[agreementName]; ok {
											logger.Debugf("%s has %d plugins in agreement '%s' and %d intents", t.memberlist.LocalNode().Name, len(t.agreements[agreementName].PluginAgreement.Plugins), agreementName, len(t.intentBuffer))
											if ok, _ := a.PluginAgreement.Plugins.contains(plugin); ok {
												return
											}
										}
										logger.Debugf("%s has %d intents", t.memberlist.LocalNode().Name, len(t.intentBuffer))
										time.Sleep(50 * time.Millisecond)
									}
								}(t)
							}
							wg.Wait()
							// time.Sleep(1 * time.Second)
							for _, t := range tribes {
								So(len(t.intentBuffer), ShouldEqual, 0)
								So(len(t.agreements[agreementName].PluginAgreement.Plugins),
									ShouldEqual, 1)
							}

							Convey("being added to an agreement it already belongs to", func() {
								err := t.JoinAgreement(agreementName, t.memberlist.LocalNode().Name)
								So(err.Error(), ShouldResemble, errAlreadyMemberOfPluginAgreement.Error())

								Convey("leaving an agreement that doesn't exist", func() {
									err := t.LeaveAgreement("whatever", t.memberlist.LocalNode().Name)
									So(err.Error(), ShouldResemble, errAgreementDoesNotExist.Error())

									Convey("an unknown member trying to leave an agreement", func() {
										err := t.LeaveAgreement(agreementName, "whatever")
										So(err.Error(), ShouldResemble, errUnknownMember.Error())

										Convey("a member leaving an agreement it isn't part of", func() {
											i := (int(t.memberlist.LocalNode().Port) + 1) % numOfTribes
											err := t.LeaveAgreement(agreementName, tribes[i].memberlist.LocalNode().Name)
											So(err.Error(), ShouldResemble, errNotAMember.Error())

											Convey("an unknown member trying to join an agreement", func() {
												msg := &agreementMsg{
													LTime:         t.clock.Time(),
													UUID:          uuid.New(),
													AgreementName: agreementName,
													MemberName:    "whatever",
													Type:          joinAgreementMsgType,
												}
												err := t.joinAgreement(msg)
												So(err, ShouldNotBeNil)
												So(err.Error(), ShouldResemble, errUnknownMember.Error())

												Convey("leaving an agreement", func() {
													So(len(t.agreements[agreementName].Members), ShouldEqual, 1)
													So(t.members[t.memberlist.LocalNode().Name].PluginAgreement, ShouldNotBeNil)
													err := t.LeaveAgreement(agreementName, t.memberlist.LocalNode().Name)
													So(err, ShouldBeNil)
													So(len(t.agreements[agreementName].Members), ShouldEqual, 0)
													So(t.members[t.memberlist.LocalNode().Name].PluginAgreement, ShouldBeNil)
													Convey("removes an agreement", func() {
														err := t.RemoveAgreement(agreementName)
														So(err, ShouldBeNil)
														So(len(t.agreements), ShouldEqual, 0)
														Convey("removes an agreement that no longer exists", func() {
															err := t.RemoveAgreement(agreementName)
															So(err.Error(), ShouldResemble, errAgreementDoesNotExist.Error())
															So(len(t.agreements), ShouldEqual, 0)
														})
													})
												})
											})
										})
									})
								})
							})
						})
					})
				})
			})
		})
	})
}

func TestTribeMembership(t *testing.T) {
	numOfTribes := 5
	tribes := getTribes(numOfTribes, 8200, nil)
	Convey(fmt.Sprintf("%d tribes are started", numOfTribes), t, func() {
		for i := 0; i < numOfTribes; i++ {
			log.Debugf("%v is reporting %v members", i, len(tribes[i].memberlist.Members()))
			So(len(tribes[0].memberlist.Members()), ShouldEqual, len(tribes[i].memberlist.Members()))
		}
		Convey("The cluster agrees on membership", func() {
			for i := 0; i < numOfTribes; i++ {

				So(
					len(tribes[0].memberlist.Members()),
					ShouldEqual,
					len(tribes[i].memberlist.Members()),
				)
				So(len(tribes[0].members), ShouldEqual, len(tribes[i].members))
			}
			Convey("Adds an agreement", func(c C) {
				agreement := "agreement1"
				t := tribes[numOfTribes-1]
				t.AddAgreement("agreement1")
				var wg sync.WaitGroup
				for _, t := range tribes {
					wg.Add(1)
					go func(t *tribe) {
						defer wg.Done()
						for {
							if t.agreements != nil {
								if _, ok := t.agreements[agreement]; ok {
									c.So(ok, ShouldEqual, true)
									return
								}
								logger.Debugf(
									"%v has %d agreements",
									t.memberlist.LocalNode().Name,
									len(t.agreements),
								)
							}
							time.Sleep(50 * time.Millisecond)
						}
					}(t)
				}
				wg.Wait()
				for _, t := range tribes {
					So(len(t.agreements), ShouldEqual, 1)
					So(t.agreements[agreement], ShouldNotBeNil)
				}
				Convey("A member", func() {
					Convey("joins an agreement", func() {
						err := t.JoinAgreement(agreement, t.memberlist.LocalNode().Name)
						So(err, ShouldBeNil)
					})
					Convey("is added to an agreement it already belongs to", func() {
						Convey("adds a plugin to agreement", func() {
							err := t.AddPlugin(agreement, "plugin1", 1)
							So(err, ShouldBeNil)
						})

						err := t.JoinAgreement(agreement, t.memberlist.LocalNode().Name)
						So(err.Error(), ShouldResemble, errAlreadyMemberOfPluginAgreement.Error())
					})
					Convey("leaves an agreement that doesn't exist", func() {
						err := t.LeaveAgreement("whatever", t.memberlist.LocalNode().Name)
						So(err.Error(), ShouldResemble, errAgreementDoesNotExist.Error())
					})
					Convey("handles an unknown member trying to leave an agreement", func() {
						err := t.LeaveAgreement(agreement, "whatever")
						So(err.Error(), ShouldResemble, errUnknownMember.Error())
					})
					Convey("handles a member leaving an agreement it isn't part of", func() {
						err := t.LeaveAgreement(agreement, tribes[0].memberlist.LocalNode().Name)
						So(err.Error(), ShouldResemble, errNotAMember.Error())
					})
					Convey("handles an unknown member trying to join an agreement", func() {
						msg := &agreementMsg{
							LTime:         t.clock.Time(),
							UUID:          uuid.New(),
							AgreementName: agreement,
							MemberName:    "whatever",
							Type:          joinAgreementMsgType,
						}
						err := t.joinAgreement(msg)
						So(err, ShouldNotBeNil)
						So(err.Error(), ShouldResemble, errUnknownMember.Error())
					})
				})
			})
		})

	})
}

func TestTribePluginAgreement(t *testing.T) {
	numOfTribes := 5
	tribePort := 8100
	tribes := getTribes(numOfTribes, tribePort, nil)
	Convey(fmt.Sprintf("%d tribes are started", numOfTribes), t, func() {
		for i := 0; i < numOfTribes; i++ {
			So(
				len(tribes[0].memberlist.Members()),
				ShouldEqual,
				len(tribes[i].memberlist.Members()),
			)
			logger.Debugf("%v has %v members", tribes[i].memberlist.LocalNode().Name, len(tribes[i].members))
			So(len(tribes[i].members), ShouldEqual, numOfTribes)
		}

		Convey("The cluster agrees on membership", func() {
			for i := 0; i < numOfTribes; i++ {
				log.Debugf("%v is reporting %v members", i, len(tribes[i].memberlist.Members()))
				So(len(tribes[0].memberlist.Members()), ShouldEqual, len(tribes[i].memberlist.Members()))
				So(len(tribes[0].members), ShouldEqual, len(tribes[i].members))
			}
			oldMember := tribes[0]
			err := tribes[0].memberlist.Leave(2 * time.Second)
			// err := tribes[0].memberlist.Shutdown()
			So(err, ShouldBeNil)
			tribes = append(tribes[:0], tribes[1:]...)

			Convey("Membership decreases as members leave", func(c C) {
				wg := sync.WaitGroup{}
				for i := range tribes {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						for {
							if len(tribes[i].members) == len(tribes) {
								c.So(len(tribes[i].members), ShouldEqual, len(tribes))
								return
							}
							time.Sleep(20 * time.Millisecond)
						}
					}(i)
				}
				wg.Wait()
				err := oldMember.memberlist.Shutdown()
				So(err, ShouldBeNil)
				So(len(tribes[rand.Intn(len(tribes))].memberlist.Members()), ShouldEqual, len(tribes))
				So(len(tribes[1].members), ShouldEqual, len(tribes))

				Convey("Membership increases as members join", func(c C) {
					conf := DefaultConfig(fmt.Sprintf("member-%d", numOfTribes+1), "127.0.0.1", tribePort+numOfTribes+1, fmt.Sprintf("127.0.0.1:%d", tribePort+2))
					tr, err := New(conf)
					if err != nil {
						So(err, ShouldBeNil)
					}
					tribes = append(tribes, tr)

					wg := sync.WaitGroup{}
					for i := range tribes {
						wg.Add(1)
						go func(i int) {
							defer wg.Done()
							for {
								if len(tribes[i].memberlist.Members()) == len(tribes) {
									c.So(len(tribes[i].members), ShouldEqual, len(tribes))
									return
								}
								time.Sleep(20 * time.Millisecond)
							}
						}(i)
					}
					wg.Wait()
					So(len(tribes[rand.Intn(len(tribes))].memberlist.Members()), ShouldEqual, len(tribes))
					So(len(tribes[rand.Intn(len(tribes))].members), ShouldEqual, len(tribes))

					Convey("Handles a 'add agreement' message broadcasted across the cluster", func(c C) {
						tribes[0].AddAgreement("clan1")
						var wg sync.WaitGroup
						for _, t := range tribes {
							wg.Add(1)
							go func(t *tribe) {
								defer wg.Done()
								for {
									if t.agreements != nil {
										if _, ok := t.agreements["clan1"]; ok {
											c.So(ok, ShouldEqual, true)
											return
										}
									}
									time.Sleep(50 * time.Millisecond)
								}
							}(t)
						}
						wg.Wait()

						numAddMessages := 10
						Convey(fmt.Sprintf("Handles %d plugin 'add messages' broadcasted across the cluster", numAddMessages), func() {
							for i := 0; i < numAddMessages; i++ {
								tribes[0].AddPlugin("clan1", fmt.Sprintf("plugin%v", i), 1)
								// time.Sleep(time.Millisecond * 50)
							}
							wg := sync.WaitGroup{}
							for _, tr := range tribes {
								wg.Add(1)
								go func(tr *tribe) {
									defer wg.Done()
									for {
										if clan, ok := tr.agreements["clan1"]; ok {
											if len(clan.PluginAgreement.Plugins) == numAddMessages {
												return
											}
											time.Sleep(50 * time.Millisecond)
											log.Debugf("%v has %v of %v plugins and %d intents\n", tr.memberlist.LocalNode().Name, len(clan.PluginAgreement.Plugins), numAddMessages, len(tr.intentBuffer))
										}
									}
								}(tr)

							}
							log.Debugf("Waits for %d members of clan1 to have %d plugins\n", numOfTribes, numAddMessages)
							wg.Wait()
							for i := 0; i < numOfTribes; i++ {
								So(len(tribes[i].agreements["clan1"].PluginAgreement.Plugins), ShouldEqual, numAddMessages)
								logger.Debugf("%v has %v intents\n", tribes[i].memberlist.LocalNode().Name, len(tribes[i].intentBuffer))
								So(len(tribes[i].intentBuffer), ShouldEqual, 0)
								for k, v := range tribes[i].intentBuffer {
									logger.Debugf("\tadd intent %v %v\n", k, v)

								}
							}

							Convey("Handles duplicate 'add plugin' messages", func() {
								t := tribes[rand.Intn(numOfTribes)]
								msg := &pluginMsg{
									Plugin: plugin{
										Name:    "pluginABC",
										Version: 1,
									},
									UUID:          uuid.New(),
									AgreementName: "clan1",
									LTime:         t.clock.Time(),
									Type:          addPluginMsgType,
								}
								So(len(t.intentBuffer), ShouldEqual, 0)
								t.handleAddPlugin(msg)
								before := len(t.agreements["clan1"].PluginAgreement.Plugins)
								t.handleAddPlugin(msg)
								after := len(t.agreements["clan1"].PluginAgreement.Plugins)
								So(before, ShouldEqual, after)

								Convey("Handles out-of-order 'add plugin' messages", func() {
									msg := &pluginMsg{
										Plugin: plugin{
											Name:    "pluginABC",
											Version: 1,
										},
										UUID:          uuid.New(),
										AgreementName: "clan1",
										LTime:         t.clock.Time(),
										Type:          addPluginMsgType,
									}
									t.handleAddPlugin(msg)
									So(len(t.intentBuffer), ShouldEqual, 1)

									Convey("Handles duplicate out-of-order 'add plugin' messages", func() {
										before := len(t.agreements["clan1"].PluginAgreement.Plugins)
										t.handleAddPlugin(msg)
										after := len(t.agreements["clan1"].PluginAgreement.Plugins)
										So(before, ShouldEqual, after)
										So(len(t.intentBuffer), ShouldEqual, 1)

										So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldBeGreaterThan, numAddMessages)
										t.handleRemovePlugin(&pluginMsg{
											LTime:         t.clock.Time(),
											Plugin:        plugin{Name: "pluginABC", Version: 1},
											AgreementName: "clan1",
											Type:          removePluginMsgType,
										})
										time.Sleep(50 * time.Millisecond)
										So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldBeGreaterThan, numAddMessages)
										So(len(t.intentBuffer), ShouldEqual, 0)
										t.handleRemovePlugin(&pluginMsg{
											LTime:         t.clock.Time(),
											Plugin:        plugin{Name: "pluginABC", Version: 1},
											AgreementName: "clan1",
											Type:          removePluginMsgType,
										})
										time.Sleep(50 * time.Millisecond)
										So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldEqual, numAddMessages)

										Convey("Handles a 'remove plugin' messages broadcasted across the cluster", func(c C) {
											for _, t := range tribes {
												So(len(t.intentBuffer), ShouldEqual, 0)
												So(len(t.intentBuffer), ShouldEqual, 0)
												So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldEqual, numAddMessages)
											}
											t := tribes[rand.Intn(numOfTribes)]
											plugin := t.agreements["clan1"].PluginAgreement.Plugins[rand.Intn(numAddMessages)]
											before := len(t.agreements["clan1"].PluginAgreement.Plugins)
											t.RemovePlugin("clan1", plugin.Name, plugin.Version)
											after := len(t.agreements["clan1"].PluginAgreement.Plugins)
											So(before-after, ShouldEqual, 1)
											var wg sync.WaitGroup
											for _, t := range tribes {
												wg.Add(1)
												go func(t *tribe) {
													defer wg.Done()
													for {
														select {
														case <-time.After(1500 * time.Millisecond):
															c.So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldEqual, after)
														default:
															if len(t.agreements["clan1"].PluginAgreement.Plugins) == after {
																c.So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldEqual, after)
																return
															}
															time.Sleep(50 * time.Millisecond)
														}
													}
												}(t)
											}
											wg.Done()

											Convey("Handles out-of-order remove", func() {
												t := tribes[rand.Intn(numOfTribes)]
												plugin := t.agreements["clan1"].PluginAgreement.Plugins[rand.Intn(numAddMessages-1)]
												msg := &pluginMsg{
													LTime:         t.clock.Increment(),
													Plugin:        plugin,
													AgreementName: "clan1",
													UUID:          uuid.New(),
													Type:          removePluginMsgType,
												}
												before := len(t.agreements["clan1"].PluginAgreement.Plugins)
												t.handleRemovePlugin(msg)
												So(before-1, ShouldEqual, len(t.agreements["clan1"].PluginAgreement.Plugins))
												before = len(t.agreements["clan1"].PluginAgreement.Plugins)
												msg.UUID = uuid.New()
												msg.LTime = t.clock.Increment()
												t.handleRemovePlugin(msg)
												after := len(t.agreements["clan1"].PluginAgreement.Plugins)
												So(before, ShouldEqual, after)
												So(len(t.intentBuffer), ShouldEqual, 1)

												Convey("Handles duplicate out-of-order 'remove plugin' messages", func() {
													t.handleRemovePlugin(msg)
													after := len(t.agreements["clan1"].PluginAgreement.Plugins)
													So(before, ShouldEqual, after)
													So(len(t.intentBuffer), ShouldEqual, 1)

													t.handleAddPlugin(&pluginMsg{
														LTime:         t.clock.Increment(),
														Plugin:        plugin,
														AgreementName: "clan1",
														Type:          addPluginMsgType,
													})
													So(len(t.intentBuffer), ShouldEqual, 0)
													ok, _ := t.agreements["clan1"].PluginAgreement.Plugins.contains(msg.Plugin)
													So(ok, ShouldBeFalse)

													Convey("Handles old 'remove plugin' messages", func() {
														t := tribes[rand.Intn(numOfTribes)]
														plugin := t.agreements["clan1"].PluginAgreement.Plugins[rand.Intn(len(t.agreements["clan1"].PluginAgreement.Plugins))]
														msg := &pluginMsg{
															LTime:         LTime(1025),
															Plugin:        plugin,
															AgreementName: "clan1",
															UUID:          uuid.New(),
															Type:          removePluginMsgType,
														}
														before := len(t.agreements["clan1"].PluginAgreement.Plugins)
														t.handleRemovePlugin(msg)
														after := len(t.agreements["clan1"].PluginAgreement.Plugins)
														So(before-1, ShouldEqual, after)
														msg2 := &pluginMsg{
															LTime:         LTime(513),
															Plugin:        plugin,
															AgreementName: "clan1",
															UUID:          uuid.New(),
															Type:          addPluginMsgType,
														}
														before = len(t.agreements["clan1"].PluginAgreement.Plugins)
														t.handleAddPlugin(msg2)
														after = len(t.agreements["clan1"].PluginAgreement.Plugins)
														So(before, ShouldEqual, after)
														msg3 := &pluginMsg{
															LTime:         LTime(513),
															Plugin:        plugin,
															AgreementName: "clan1",
															UUID:          uuid.New(),
															Type:          removePluginMsgType,
														}
														before = len(t.agreements["clan1"].PluginAgreement.Plugins)
														t.handleRemovePlugin(msg3)
														after = len(t.agreements["clan1"].PluginAgreement.Plugins)
														So(before, ShouldEqual, after)

														Convey("The tribe agrees on plugin agreements", func(c C) {
															var wg sync.WaitGroup
															for _, t := range tribes {
																wg.Add(1)
																go func(t *tribe) {
																	for {
																		defer wg.Done()
																		select {
																		case <-time.After(1 * time.Second):
																			c.So(len(t.memberlist.Members()), ShouldEqual, numOfTribes)
																		default:
																			if len(t.agreements["clan1"].PluginAgreement.Plugins) == numAddMessages-1 {
																				c.So(len(t.agreements["clan1"].PluginAgreement.Plugins), ShouldEqual, numAddMessages-1)
																				return
																			}
																		}

																	}
																}(t)
															}
															wg.Done()
														})
													})
												})
											})
										})
									})
								})
							})
						})
					})
				})
			})
		})
	})
}

// seedTribe and conf can be nil
func getTribes(numOfTribes, port int, seedTribe *tribe) []*tribe {
	tribes := []*tribe{}
	wg := sync.WaitGroup{}
	var seed string
	if seedTribe != nil {
		tribes = append(tribes, seedTribe)
		seed = fmt.Sprintf("%v:%v", seedTribe.memberlist.LocalNode().Addr, seedTribe.memberlist.LocalNode().Port)
	}
	fmt.Printf("Starting loop for %d\n", port)
	for i := 0; i < numOfTribes; i++ {
		if seedTribe != nil && i == 0 {
			continue
		}
		if i > 0 && seedTribe == nil {
			seed = fmt.Sprintf("127.0.0.1:%d", port)
		}
		conf := DefaultConfig(fmt.Sprintf("member-%v", i), "127.0.0.1", port+i, seed)
		tr, err := New(conf)
		if err != nil {
			panic(err)
		}
		tribes = append(tribes, tr)
		wg.Add(1)
		to := time.After(4 * time.Second)
		go func(tr *tribe) {
			defer wg.Done()
			for {
				select {
				case <-to:
					panic("Timed out while establishing membership")
				default:
					if len(tr.memberlist.Members()) == numOfTribes {
						return
					}
					log.Debugf("%v has %v of %v members", tr.memberlist.LocalNode().Name, len(tr.memberlist.Members()), numOfTribes)
					members := []string{}
					for _, m := range tr.memberlist.Members() {
						members = append(members, m.Name)
					}
					log.Debugf("%v has %v members", tr.memberlist.LocalNode().Name, members)
					time.Sleep(50 * time.Millisecond)
				}
			}
		}(tr)
	}
	wg.Wait()
	return tribes
}