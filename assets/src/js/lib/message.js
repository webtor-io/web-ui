import {makeDebug} from './debug';
const debug = await makeDebug('webtor:embed:message');
function inIframe() {
  try {
      return window.self !== window.top;
  } catch (e) {
      return true;
  }
}
const id = window._id;
debug('using message id=%o', id);
const message = {
  id() {
    return id;
  },
  send(m, data = {}) {
    if (!inIframe) return;
    if (!id) {
      m = 'webtor: ' + m;
    } else {
      m = {
        id,
        name: m,
        data,
      };
    }
    debug('post message=%o data=%o', m, data);
    window.parent.postMessage(m, '*');
  },
  receiveOnce(name) {
    return new Promise((resolve, reject) => {
      const func = (event) => {
        const d = event.data;
        if (!id) {
          window.removeEventListener('message', func);
          resolve();
        }
        if (d.id == id && d.name == name) {
            debug('receive message=%o', d);
            window.removeEventListener('message', func);
            resolve(d.data);
        }
      }
      window.addEventListener('message', func);
    });
  },
  receive(name, callback) {
    window.addEventListener('message', function(event) {
        const d = event.data;
        if (d.id == id && d.name == name) {
            debug('receive message=%o', d);
            callback(d.data);
        }
    });
  }
}
export default message;