using System.ComponentModel;
using System.Drawing;
using System.Windows;
using HypoMux.App.ViewModels;
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
        if (App.StartHidden)
        {
            Hide();
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
