describe('@features/settings public surface', () => {
  it('re-exports the same SettingsScreen as the ui module', () => {
    expect(require('@features/settings').SettingsScreen).toBe(
      require('@features/settings/ui/SettingsScreen').SettingsScreen,
    );
  });

  it('exports exactly SettingsScreen', () => {
    expect(Object.keys(require('@features/settings'))).toEqual(['SettingsScreen']);
  });
});
