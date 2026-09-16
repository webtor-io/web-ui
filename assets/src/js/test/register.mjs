// `npm test` passes this as --import, so the hooks in ./jsx-hooks.mjs are
// installed before any test file is loaded. Nothing else belongs here: the
// hooks run on a separate thread and cannot see this module's state, and a
// DOM set up here would be a global every plain-JS test file inherits
// whether it wants one or not (Player.wiring.test.js builds its own).
import { register } from 'node:module';

register('./jsx-hooks.mjs', import.meta.url);
