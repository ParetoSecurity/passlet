import AppKit

enum MenuIcon {
    static let image: NSImage = {
        let image = NSImage(size: NSSize(width: 22, height: 18), flipped: false) { _ in
            guard let context = NSGraphicsContext.current?.cgContext else { return false }
            context.saveGState()
            defer { context.restoreGState() }
            context.setFillColor(NSColor.black.cgColor)
            context.addPath(CGPath(roundedRect: CGRect(x: 1, y: 1, width: 20, height: 16),
                                   cornerWidth: 3, cornerHeight: 3, transform: nil))
            context.fillPath()
            context.setBlendMode(.clear)
            for x in [CGFloat(1), CGFloat(21)] {
                context.fillEllipse(in: CGRect(x: x - 2.5, y: 6.5, width: 5, height: 5))
            }
            context.fillEllipse(in: CGRect(x: 8.5, y: 8.5, width: 5, height: 5))
            context.move(to: CGPoint(x: 10, y: 10))
            context.addLine(to: CGPoint(x: 8.5, y: 4.5))
            context.addLine(to: CGPoint(x: 13.5, y: 4.5))
            context.addLine(to: CGPoint(x: 12, y: 10))
            context.closePath()
            context.fillPath()
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = "Passlet"
        return image
    }()
}
