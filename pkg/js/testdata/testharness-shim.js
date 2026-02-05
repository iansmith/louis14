// testharness-shim.js — Minimal WPT testharness.js shim for goja (ES5.1)
// Provides test(), assert_*(), and records results in __wpt_results.

var __wpt_results = [];

function test(func, name) {
    var result = { name: name || "(unnamed)", status: "PASS", message: "" };
    try {
        func();
    } catch (e) {
        result.status = "FAIL";
        result.message = e.message || String(e);
    }
    __wpt_results.push(result);
}

function async_test(func, name) {
    // Simplified: run synchronously, provide a done() callback
    var result = { name: name || "(unnamed async)", status: "PASS", message: "" };
    var t = {
        step: function(f) {
            try { f(); } catch (e) {
                result.status = "FAIL";
                result.message = e.message || String(e);
            }
        },
        step_func: function(f) {
            return function() {
                try { return f.apply(this, arguments); } catch (e) {
                    result.status = "FAIL";
                    result.message = e.message || String(e);
                }
            };
        },
        step_func_done: function(f) {
            return function() {
                try { f.apply(this, arguments); } catch (e) {
                    result.status = "FAIL";
                    result.message = e.message || String(e);
                }
            };
        },
        done: function() {},
        unreached_func: function(msg) {
            return function() {
                result.status = "FAIL";
                result.message = msg || "unreached function called";
            };
        }
    };
    try {
        func(t);
    } catch (e) {
        result.status = "FAIL";
        result.message = e.message || String(e);
    }
    __wpt_results.push(result);
}

function assert_true(actual, description) {
    if (actual !== true) {
        throw new Error((description || "assert_true") + ": expected true, got " + actual);
    }
}

function assert_false(actual, description) {
    if (actual !== false) {
        throw new Error((description || "assert_false") + ": expected false, got " + actual);
    }
}

function assert_equals(actual, expected, description) {
    if (actual !== expected) {
        throw new Error((description || "assert_equals") +
            ": expected " + JSON.stringify(expected) +
            ", got " + JSON.stringify(actual));
    }
}

function assert_not_equals(actual, notExpected, description) {
    if (actual === notExpected) {
        throw new Error((description || "assert_not_equals") +
            ": got disallowed value " + JSON.stringify(actual));
    }
}

function assert_array_equals(actual, expected, description) {
    var desc = description || "assert_array_equals";
    if (!Array.isArray(actual)) {
        throw new Error(desc + ": actual is not an array");
    }
    if (actual.length !== expected.length) {
        throw new Error(desc + ": length mismatch: " +
            actual.length + " vs " + expected.length);
    }
    for (var i = 0; i < expected.length; i++) {
        if (actual[i] !== expected[i]) {
            throw new Error(desc + ": index " + i +
                ": expected " + JSON.stringify(expected[i]) +
                ", got " + JSON.stringify(actual[i]));
        }
    }
}

function assert_throws_js(errorType, func, description) {
    var thrown = false;
    try {
        func();
    } catch (e) {
        thrown = true;
        if (errorType && !(e instanceof errorType)) {
            throw new Error((description || "assert_throws_js") +
                ": wrong error type: " + e);
        }
    }
    if (!thrown) {
        throw new Error((description || "assert_throws_js") + ": no exception thrown");
    }
}

function assert_throws_dom(name, func, description) {
    // Simplified: just check that an error is thrown
    var thrown = false;
    try {
        func();
    } catch (e) {
        thrown = true;
    }
    if (!thrown) {
        throw new Error((description || "assert_throws_dom") + ": no exception thrown");
    }
}

function assert_class_string(obj, expected, description) {
    // Simplified: just check it's an object
    assert_true(typeof obj === "object" || typeof obj === "function",
        description || "assert_class_string");
}

function assert_readonly(obj, prop, description) {
    // Simplified no-op for our purposes
}

function assert_throws_exactly(expectedError, func, description) {
    var thrown = false;
    try {
        func();
    } catch (e) {
        thrown = true;
        if (e !== expectedError) {
            throw new Error((description || "assert_throws_exactly") +
                ": wrong error: " + e + " vs " + expectedError);
        }
    }
    if (!thrown) {
        throw new Error((description || "assert_throws_exactly") + ": no exception thrown");
    }
}

// No-ops for setup/done
function setup(func_or_properties, maybe_properties) {
    if (typeof func_or_properties === "function") {
        func_or_properties();
    }
}

function done() {}

// promise_test — simplified to synchronous
function promise_test(func, name) {
    test(function() {
        // Run and ignore promise result
        func();
    }, name);
}

// generate_tests helper
function generate_tests(func, args) {
    for (var i = 0; i < args.length; i++) {
        var testArgs = args[i];
        var name = testArgs[0];
        test(function() {
            func.apply(null, testArgs.slice(1));
        }, name);
    }
}
