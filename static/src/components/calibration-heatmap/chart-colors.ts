// Series colors of pitha-calibration-heatmap are tokens (lib/chart-theme.ts): the observed
// accuracy in --ink, the perfect-calibration reference in --mute (brand-guide §8: no blue,
// no prism). Read when the chart is created and again on an OS Light/Dark switch.
import { readToken } from '../lib/chart-theme';

export const perfectColor = () => ({ color: readToken('mute') });
export const accuracyColor = () => ({ color: readToken('ink') });
