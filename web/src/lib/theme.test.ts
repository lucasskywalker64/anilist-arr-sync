import { describe, it, expect } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';

describe('Sonarr Dark Theme Tokens & Dimensions', () => {
  it('defines all required colors, tokens, and geometry in theme.css', () => {
    const cssPath = path.resolve(__dirname, '../theme.css');
    const css = fs.readFileSync(cssPath, 'utf-8');

    const requiredTokens = [
      '#202020', // pageBackground
      '#2a2a2a', // pageHeaderBackgroundColor / sidebarBackgroundColor / cardCenterBackgroundColor
      '#262626', // toolbarBackgroundColor
      '#35c5f4', // sonarrBlue / themeBlue
      '#2193b5', // themeAlternateBlue
      '#5d9cec', // primaryColor / infoColor
      '#dde6e9', // inputBorderColor
      '#333333', // cardBackgroundColor / sidebarActiveBackgroundColor
      '#909293', // helpTextColor
      '210px',   // sidebarWidth
      '60px',    // headerHeight / toolbarHeight / toolbarButtonWidth
      '250px',   // formLabelLargeWidth
      '35px',    // input height
    ];

    for (const token of requiredTokens) {
      expect(css).toContain(token);
    }

    // Verify key CSS custom properties exist
    const customProps = [
      '--sonarrBlue',
      '--themeBlue',
      '--themeAlternateBlue',
      '--primaryColor',
      '--pageBackground',
      '--pageHeaderBackgroundColor',
      '--sidebarBackgroundColor',
      '--sidebarActiveBackgroundColor',
      '--toolbarBackgroundColor',
      '--inputBorderColor',
      '--helpTextColor',
      '--sidebarWidth',
      '--headerHeight',
      '--toolbarHeight',
      '--formLabelLargeWidth',
    ];

    for (const prop of customProps) {
      expect(css).toContain(prop);
    }
  });
});
