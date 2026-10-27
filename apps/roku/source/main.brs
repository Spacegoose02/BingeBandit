' BingeBandit for Roku. Everything happens in MainScene; this just opens it
' and keeps the channel alive until the scene closes.
sub Main(args as dynamic)
    screen = CreateObject("roSGScreen")
    port = CreateObject("roMessagePort")
    screen.setMessagePort(port)
    m.global = screen.getGlobalNode()
    m.global.addFields({ server: "", token: "", user: {}, profileChosen: false, unauthorized: false })
    scene = screen.CreateScene("MainScene")
    screen.show()
    ' Roku's certification checks ask for this signal once the first screen is up.
    scene.signalBeacon("AppLaunchComplete")
    while true
        msg = wait(0, port)
        if type(msg) = "roSGScreenEvent" and msg.isScreenClosed() then return
    end while
end sub
