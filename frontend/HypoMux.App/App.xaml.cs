using System.Globalization;
using System.Windows;
using Wpf.Ui.Appearance;

namespace HypoMux.App;

public partial class App : System.Windows.Application
{
    private Mutex? _singleInstance;
    private bool _ownsSingleInstance;

    public static bool StartHidden { get; private set; }

    protected override void OnStartup(StartupEventArgs e)
    {
        _singleInstance = new Mutex(
            initiallyOwned: true,
            name: @"Local\Hypostasis-Cat.HypoMux",
            createdNew: out var isFirstInstance);
        if (!isFirstInstance)
        {
            Shutdown();
            return;
        }
        _ownsSingleInstance = true;

        StartHidden = e.Args.Any(
            argument => string.Equals(
                argument,
                "--silent",
                StringComparison.OrdinalIgnoreCase));
        SelectLanguageResources();
        ApplicationThemeManager.ApplySystemTheme();
        base.OnStartup(e);
    }

    protected override void OnExit(ExitEventArgs e)
    {
        if (_ownsSingleInstance)
        {
            _singleInstance?.ReleaseMutex();
            _ownsSingleInstance = false;
        }
        _singleInstance?.Dispose();
        _singleInstance = null;
        base.OnExit(e);
    }

    private static void SelectLanguageResources()
    {
        var language = CultureInfo.CurrentUICulture.TwoLetterISOLanguageName;
        if (string.Equals(language, "zh", StringComparison.OrdinalIgnoreCase))
        {
            return;
        }

        var dictionaries = Current.Resources.MergedDictionaries;
        var existing = dictionaries.FirstOrDefault(
            dictionary => dictionary.Source?.OriginalString.Contains(
                "Strings.",
                StringComparison.OrdinalIgnoreCase) == true);
        if (existing is not null)
        {
            dictionaries.Remove(existing);
        }

        dictionaries.Add(new ResourceDictionary
        {
            Source = new Uri(
                "Resources/Strings.en-US.xaml",
                UriKind.Relative),
        });
    }
}
