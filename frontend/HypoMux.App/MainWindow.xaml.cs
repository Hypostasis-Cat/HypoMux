using System.ComponentModel;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Data;
using System.Windows.Input;
using HypoMux.App.Models;
using HypoMux.App.Services;
using HypoMux.App.ViewModels;
using Microsoft.Win32;
using Wpf.Ui.Appearance;
using Wpf.Ui.Controls;
using Forms = System.Windows.Forms;

namespace HypoMux.App;

public partial class MainWindow : FluentWindow
{
    private readonly MainWindowViewModel _viewModel;
    private readonly Forms.NotifyIcon _trayIcon;
    private bool _shutdownComplete;
    private bool _exitRequested;

    public MainWindow()
    {
        InitializeComponent();
        _viewModel = new MainWindowViewModel();
        DataContext = _viewModel;
        _trayIcon = CreateTrayIcon();
        SystemThemeWatcher.Watch(this);
        Loaded += OnLoaded;
        Closing += OnClosing;
    }

    private async void OnLoaded(object sender, RoutedEventArgs e)
    {
        Loaded -= OnLoaded;
        await _viewModel.InitializeAsync();
        TunModeButton.IsChecked = _viewModel.UseTunMode;
        ProxyModeButton.IsChecked = !_viewModel.UseTunMode;
        ApplyRoutingRuleFilter("process");
        if (App.StartHidden)
        {
            Hide();
        }
    }

    private void OnNavigateClick(object sender, RoutedEventArgs e)
    {
        if (sender is System.Windows.Controls.RadioButton { Tag: string tag }
            && int.TryParse(tag, out var index)
            && index >= 0
            && index < MainTabs.Items.Count)
        {
            MainTabs.SelectedIndex = index;
        }
    }

    private void OnProxyModeChecked(object sender, RoutedEventArgs e)
    {
        if (DataContext is MainWindowViewModel viewModel)
        {
            viewModel.UseTunMode = false;
        }
    }

    private void OnTunModeChecked(object sender, RoutedEventArgs e)
    {
        if (DataContext is MainWindowViewModel viewModel)
        {
            viewModel.UseTunMode = true;
        }
    }

    private async void OnAccelerationToggleClick(object sender, RoutedEventArgs e)
    {
        if (sender is not System.Windows.Controls.CheckBox toggle)
        {
            return;
        }

        toggle.IsEnabled = false;
        try
        {
            if (toggle.IsChecked == true)
            {
                await _viewModel.StartAccelerationAsync();
            }
            else
            {
                await _viewModel.StopAccelerationAsync();
            }
        }
        finally
        {
            toggle.IsChecked = _viewModel.IsAccelerating;
            toggle.IsEnabled = true;
        }
    }

    private void OnAddRuleClick(object sender, RoutedEventArgs e)
    {
        RuleValueTextBox.Focus();
        Keyboard.Focus(RuleValueTextBox);
    }

    private void OnRuleFilterClick(object sender, RoutedEventArgs e)
    {
        if (sender is System.Windows.Controls.RadioButton { Tag: string matchType })
        {
            ApplyRoutingRuleFilter(matchType);
            _viewModel.NewRuleMatchType = matchType;
        }
    }

    private void ApplyRoutingRuleFilter(string matchType)
    {
        var view = CollectionViewSource.GetDefaultView(
            _viewModel.RoutingRules);
        view.Filter = item =>
            item is RoutingRuleOption rule
            && string.Equals(
                rule.MatchType,
                matchType,
                StringComparison.OrdinalIgnoreCase);
        view.Refresh();
    }

    private void OnExportRulesClick(object sender, RoutedEventArgs e)
    {
        var dialog = new Microsoft.Win32.SaveFileDialog
        {
            Title = "导出 HypoMux 分流规则",
            Filter = "JSON 文件 (*.json)|*.json",
            FileName = "hypomux-rules.json",
            DefaultExt = ".json",
        };
        if (dialog.ShowDialog(this) != true)
        {
            return;
        }

        try
        {
            var rules = _viewModel.RoutingRules
                .Select(rule => new RoutingRuleSetting(
                    rule.MatchType,
                    rule.Value,
                    rule.Outbound))
                .ToArray();
            File.WriteAllText(
                dialog.FileName,
                JsonSerializer.Serialize(
                    rules,
                    new JsonSerializerOptions { WriteIndented = true }));
        }
        catch (Exception exception)
        {
            System.Windows.MessageBox.Show(
                this,
                $"导出失败：{exception.Message}",
                "HypoMux",
                System.Windows.MessageBoxButton.OK,
                System.Windows.MessageBoxImage.Error);
        }
    }

    private void OnImportRulesClick(object sender, RoutedEventArgs e)
    {
        var dialog = new Microsoft.Win32.OpenFileDialog
        {
            Title = "导入 HypoMux 分流规则",
            Filter = "JSON 文件 (*.json)|*.json",
            DefaultExt = ".json",
            CheckFileExists = true,
        };
        if (dialog.ShowDialog(this) != true)
        {
            return;
        }

        try
        {
            var rules = JsonSerializer.Deserialize<RoutingRuleSetting[]>(
                    File.ReadAllText(dialog.FileName),
                    new JsonSerializerOptions
                    {
                        PropertyNameCaseInsensitive = true,
                    })
                ?? [];
            foreach (var rule in rules)
            {
                if (string.IsNullOrWhiteSpace(rule.Value))
                {
                    continue;
                }

                _viewModel.RoutingRules.Add(new RoutingRuleOption
                {
                    MatchType = rule.MatchType,
                    Value = rule.Value,
                    Outbound = rule.Outbound,
                });
            }

            CollectionViewSource
                .GetDefaultView(_viewModel.RoutingRules)
                .Refresh();
        }
        catch (Exception exception)
        {
            System.Windows.MessageBox.Show(
                this,
                $"导入失败：{exception.Message}",
                "HypoMux",
                System.Windows.MessageBoxButton.OK,
                System.Windows.MessageBoxImage.Error);
        }
    }

    private void OnOpenGitHubClick(
        object sender,
        RoutedEventArgs e) =>
        OpenUrl("https://github.com/Hypostasis-Cat/HypoMux");

    private void OnOpenReleasesClick(
        object sender,
        RoutedEventArgs e) =>
        OpenUrl("https://github.com/Hypostasis-Cat/HypoMux/releases/latest");

    private static void OpenUrl(string url)
    {
        try
        {
            Process.Start(new ProcessStartInfo(url)
            {
                UseShellExecute = true,
            });
        }
        catch (Exception)
        {
        }
    }

    private async void OnClosing(object? sender, CancelEventArgs e)
    {
        if (_shutdownComplete)
        {
            return;
        }

        if (!_exitRequested && _viewModel.CloseToTray)
        {
            e.Cancel = true;
            Hide();
            return;
        }

        e.Cancel = true;
        IsEnabled = false;
        try
        {
            await _viewModel.DisposeAsync();
        }
        finally
        {
            SystemThemeWatcher.UnWatch(this);
            _trayIcon.Visible = false;
            _trayIcon.Dispose();
            _shutdownComplete = true;
            System.Windows.Application.Current.Shutdown();
        }
    }

    private void OnSystemThemeClick(object sender, RoutedEventArgs e)
    {
        SystemThemeWatcher.Watch(this);
        ApplicationThemeManager.ApplySystemTheme();
    }

    private void OnLightThemeClick(object sender, RoutedEventArgs e)
    {
        SystemThemeWatcher.UnWatch(this);
        ApplicationThemeManager.Apply(
            ApplicationTheme.Light,
            WindowBackdropType.Mica);
    }

    private void OnDarkThemeClick(object sender, RoutedEventArgs e)
    {
        SystemThemeWatcher.UnWatch(this);
        ApplicationThemeManager.Apply(
            ApplicationTheme.Dark,
            WindowBackdropType.Mica);
    }

    private Forms.NotifyIcon CreateTrayIcon()
    {
        var menu = new Forms.ContextMenuStrip();
        menu.Items.Add(
            "显示 HypoMux",
            image: null,
            (_, _) => ShowFromTray());
        menu.Items.Add(
            "启动加速",
            image: null,
            (_, _) => Dispatcher.BeginInvoke(
                () => _viewModel.StartCommand.Execute(null)));
        menu.Items.Add(
            "停止加速",
            image: null,
            (_, _) => Dispatcher.BeginInvoke(
                () => _viewModel.StopCommand.Execute(null)));
        menu.Items.Add(new Forms.ToolStripSeparator());
        menu.Items.Add(
            "退出",
            image: null,
            (_, _) => Dispatcher.Invoke(RequestExit));

        var icon = Environment.ProcessPath is { } processPath
            ? System.Drawing.Icon.ExtractAssociatedIcon(processPath)
            : SystemIcons.Application;
        var notifyIcon = new Forms.NotifyIcon
        {
            Text = "HypoMux",
            Icon = icon ?? SystemIcons.Application,
            ContextMenuStrip = menu,
            Visible = true,
        };
        notifyIcon.DoubleClick += (_, _) => Dispatcher.Invoke(ShowFromTray);
        return notifyIcon;
    }

    private void ShowFromTray()
    {
        Show();
        WindowState = WindowState.Normal;
        Activate();
    }

    private void RequestExit()
    {
        _exitRequested = true;
        Close();
    }
}
