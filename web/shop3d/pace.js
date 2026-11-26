// pace keeps the walk fluid on a slow device: when the frames drawn come late on average, it lowers
// the canvas's pixel ratio a step, down to lo. It never climbs back during a visit, so the picture
// does not keep changing; enter() starts a new one. No three.js here: node tests it.
const STEP = 0.25, LATE = 24, FRAMES = 90, WAIT = 3000; // ms a frame may take; frames averaged; ms let pass after the start and each step

export function pace(lo, hi) {
  let ratio = hi, since = null, sum = 0, n = 0;
  return {
    get ratio() { return ratio; },
    // frame takes how long a drawn frame took (ms) and when it ended; it returns the new ratio when it
    // changes, else null.
    frame(ms, now) {
      since ??= now;
      if (now - since < WAIT || ratio <= lo) return null;
      sum += ms;
      if (++n < FRAMES) return null;
      const late = sum / n > LATE;
      sum = n = 0;
      if (!late) return null;
      ratio = Math.max(lo, ratio - STEP);
      since = now;
      return ratio;
    },
  };
}
