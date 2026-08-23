// Added by the desuqcafe fork. Lives in its own directory, which upstream does
// not have, so it can never conflict on a merge.
//
// Say, next to a rate limit, whether that rate limit is actually going to do
// anything.
//
// `limitBandwidthInLan` defaults to false (lib/config/optionsconfiguration.go),
// and when it is off the limiter skips every connection it considers local:
//
//     // lib/connections/limiter.go
//     if w.isLAN && !w.limitsLAN.Load() { ... unlimited ... }
//
// with `isLAN` coming from `c.IsLocal()` in lib/connections/service.go. So
// somebody who sets a limit and then tests it against a machine on their own
// desk -- or, worse, against a second instance on loopback -- sees no effect
// whatsoever and concludes rate limiting is broken. Nothing in the GUI says a
// word about it. The checkbox exists, three fields down in one place and in a
// different dialogue entirely in the other, and its name does not read as a
// precondition for the fields above it.
//
// The default itself is left alone, deliberately. Throttling LAN transfers is
// the wrong thing for a studio moving tens of gigabytes of textures between
// machines in one room, and for anyone whose colleagues are remote the setting
// changes nothing at all -- those connections are not local. The problem was
// never the default; it was that the default was invisible.
//
// Where a device ID is given, the note is not hypothetical: /rest/system/
// connections reports `isLocal` per connection, from the same IsLocal() the
// limiter asks, so it can say whether the limits are being skipped for this
// device right now.

angular.module('syncthing.core')
    .directive('desuqLanLimit', function () {
        return {
            restrict: 'E',
            scope: {
                // The two rate fields this note sits under. There is nothing
                // to warn about until one of them is set.
                recv: '=',
                send: '=',
                // The current value of options.limitBandwidthInLan. Bound to
                // the editor's working copy where there is one, so the note
                // reacts to the checkbox before anything is saved.
                inLan: '=',
                // Optional: the device these limits belong to. Given one, the
                // note reports what is happening on the live connection.
                deviceId: '=?',
                // The map from /rest/system/connections.
                connections: '=?',
                // Where the checkbox lives, when it is not on this screen.
                elsewhere: '@'
            },
            // Plain {{ }} rather than the translate directive: angular-translate
            // renders {%placeholders%} literally for any string it has no entry
            // for, and none of these will ever be in upstream's language files.
            template:
                '<div class="desuq-lanlimit" ng-if="limited"' +
                '     ng-class="inLan ? \'desuq-lanlimit-on\' : \'desuq-lanlimit-off\'">' +
                '  <span class="fas fa-fw" ng-class="inLan ? \'fa-check-circle\' : \'fa-info-circle\'"></span>' +
                '  <span ng-if="inLan">' +
                '    Applies to every connection, including devices on this network.' +
                '  </span>' +
                '  <span ng-if="!inLan">' +
                '    <strong>Not applied on the local network.</strong>' +
                '    Only traffic that leaves this network is limited.' +
                '    <span ng-if="elsewhere">Change that with &ldquo;Limit Bandwidth in LAN&rdquo; in {{elsewhere}}.</span>' +
                '    <span ng-if="!elsewhere">Tick &ldquo;Limit Bandwidth in LAN&rdquo; below to change that.</span>' +
                '  </span>' +
                '  <span class="desuq-lanlimit-live" ng-if="local === true && !inLan">' +
                '    This device is connected over the local network right now, so nothing is being limited.' +
                '  </span>' +
                '  <span class="desuq-lanlimit-live" ng-if="local === false && !inLan">' +
                '    This device is not on the local network, so the limits are in force.' +
                '  </span>' +
                '</div>',
            link: function (scope) {
                function evaluate() {
                    scope.limited = Number(scope.recv) > 0 || Number(scope.send) > 0;

                    // Undefined rather than false when there is nothing to go
                    // on, so the template can leave the sentence out entirely
                    // instead of asserting something it does not know.
                    scope.local = undefined;
                    if (!scope.deviceId || !scope.connections) {
                        return;
                    }
                    var conn = scope.connections[scope.deviceId];
                    if (conn && conn.connected) {
                        scope.local = !!conn.isLocal;
                    }
                }

                scope.$watch('recv', evaluate);
                scope.$watch('send', evaluate);
                scope.$watch('inLan', evaluate);
                scope.$watch('deviceId', evaluate);
                scope.$watch(function () {
                    var conn = scope.connections && scope.deviceId
                        ? scope.connections[scope.deviceId]
                        : null;
                    return conn ? '' + conn.connected + conn.isLocal : '';
                }, evaluate);
            }
        };
    });
