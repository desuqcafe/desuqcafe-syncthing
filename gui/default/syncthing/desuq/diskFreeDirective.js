// Added by the desuqcafe fork. Lives in its own directory, which upstream does
// not have, so it can never conflict on a merge. The only upstream edits it
// needs are the <script> tag that loads it and the two places it is used.
//
// Syncthing's GUI has an input for "Minimum Free Disk Space" but never shows
// how much space there actually is, and nothing anywhere compares the size of
// a folder against the drive it is being pulled onto. Someone can accept a
// 400 GB share onto a 250 GB drive and only find out when the disk is full and
// the folder is stuck at "Out of Sync". See custom/DEPLOYMENT-3D-TEAM.md.

angular.module('syncthing.core')
    .directive('diskFree', function ($http, $timeout) {
        return {
            restrict: 'E',
            scope: {
                // Directory to measure. Watched, so it follows the path field
                // as it is typed.
                path: '=',
                // Optional: bytes still to download into that path. When this
                // exceeds the free space, the shortfall is called out.
                needBytes: '=?',
                // Optional: reserve configured for the folder, so "free" can be
                // reported as what is actually usable rather than raw free.
                minDiskFree: '=?'
            },
            // Deliberately not using the translate directive. angular-translate
            // returns the key untouched when there is no entry for it, which
            // for a string with {%placeholders%} means rendering the
            // placeholders literally -- and these strings will never be in
            // upstream's translation files. Plain interpolation at least says
            // something true in English.
            template:
                '<span ng-if="usage" ng-class="{\'text-danger\': shortfall > 0, \'text-warning\': shortfall <= 0 && tight}">' +
                // fa-hdd only exists in the regular (far) set in fork-awesome.
                '<span class="far fa-hdd"></span>&nbsp;' +
                '<span ng-if="shortfall > 0">' +
                'Not enough space: needs {{shortfall | binary}}B more than the {{usage.free | binary}}B free here' +
                '</span>' +
                '<span ng-if="shortfall <= 0">' +
                '{{usage.free | binary}}B free of {{usage.total | binary}}B' +
                '</span>' +
                '</span>',
            link: function (scope) {
                var pending = null;

                scope.usage = null;
                scope.shortfall = -1;
                scope.tight = false;

                function evaluate() {
                    if (!scope.usage) {
                        return;
                    }
                    var free = scope.usage.free;
                    var need = Number(scope.needBytes) || 0;

                    scope.shortfall = need > free ? need - free : -1;
                    // Within 10% of filling the drive is worth a nudge even
                    // when it technically fits.
                    scope.tight = need > 0 && need > free * 0.9;
                }

                function refresh(path) {
                    if (!path) {
                        scope.usage = null;
                        return;
                    }
                    $http.get(urlbase + '/system/diskfree', { params: { path: path } })
                        .then(function (response) {
                            scope.usage = response.data;
                            evaluate();
                        }, function () {
                            // A path on a drive that is not there, or not
                            // readable. Saying nothing is better than guessing.
                            scope.usage = null;
                        });
                }

                // Debounced: this is bound to a text input, and every keystroke
                // would otherwise be a stat() on the server.
                scope.$watch('path', function (path) {
                    if (pending) {
                        $timeout.cancel(pending);
                    }
                    pending = $timeout(function () {
                        refresh(path);
                    }, 400);
                });

                scope.$watch('needBytes', evaluate);

                scope.$on('$destroy', function () {
                    if (pending) {
                        $timeout.cancel(pending);
                    }
                });
            }
        };
    });
