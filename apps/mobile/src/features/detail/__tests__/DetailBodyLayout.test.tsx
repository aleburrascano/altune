import { Platform, Text } from 'react-native';
import { render, screen } from '@testing-library/react-native';

import { DetailBodyLayout } from '../ui/DetailBodyLayout';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

beforeEach(() => {
  mockWindowWidth = 390;
});

afterEach(() => {
  Platform.OS = 'ios';
  mockWindowWidth = 390;
});

function renderLayout(hero: React.ReactNode = <Text>hero-slot</Text>) {
  render(
    <DetailBodyLayout hero={hero} actions={<Text>actions-slot</Text>} facts={<Text>facts-slot</Text>}>
      <Text>children-slot</Text>
    </DetailBodyLayout>,
  );
}

describe('DetailBodyLayout: compact', () => {
  it('renders actions, facts, then children in a single column off the web platform', () => {
    Platform.OS = 'ios';
    mockWindowWidth = 1440;
    renderLayout();

    expect(screen.getByTestId('detail-body')).toBeTruthy();
    expect(screen.queryByTestId('detail-body-wide')).toBeNull();
  });

  it('stays a single column on the web at a narrow width', () => {
    Platform.OS = 'web';
    mockWindowWidth = 360;
    renderLayout();

    expect(screen.getByTestId('detail-body')).toBeTruthy();
    expect(screen.queryByTestId('detail-body-wide')).toBeNull();
  });

  it('stays a single column on the web at a wide width when there is no hero to place', () => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;
    renderLayout(null);

    expect(screen.getByTestId('detail-body')).toBeTruthy();
    expect(screen.queryByTestId('detail-body-wide')).toBeNull();
  });
});

describe('DetailBodyLayout: wide web', () => {
  it('splits into a left column with the hero and actions, and a right column with facts and children', () => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;
    renderLayout();

    expect(screen.queryByTestId('detail-body')).toBeNull();
    const left = screen.getByTestId('detail-body-left');
    const right = screen.getByTestId('detail-body-right');

    expect(left.findAllByType(Text).map((node) => node.props.children)).toEqual([
      'hero-slot',
      'actions-slot',
    ]);
    expect(right.findAllByType(Text).map((node) => node.props.children)).toEqual([
      'facts-slot',
      'children-slot',
    ]);
  });
});
