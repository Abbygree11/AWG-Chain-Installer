package main

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	ink       = color.NRGBA{R: 13, G: 15, B: 29, A: 255}
	navy      = color.NRGBA{R: 18, G: 20, B: 38, A: 255}
	slate     = color.NRGBA{R: 29, G: 30, B: 49, A: 255}
	purple    = color.NRGBA{R: 157, G: 102, B: 244, A: 255}
	purple2   = color.NRGBA{R: 109, G: 54, B: 210, A: 255}
	pale      = color.NRGBA{R: 188, G: 161, B: 245, A: 255}
	stroke    = color.NRGBA{R: 82, G: 74, B: 119, A: 255}
	secondary = color.NRGBA{R: 174, G: 172, B: 200, A: 255}
	snow      = color.NRGBA{R: 245, G: 244, B: 252, A: 255}
	mint      = color.NRGBA{R: 97, G: 218, B: 165, A: 255}
)

type chainTheme struct{}

func (chainTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return purple
	case theme.ColorNameBackground:
		return ink
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 36, G: 38, B: 60, A: 255}
	case theme.ColorNameButton:
		return slate
	case theme.ColorNameForeground:
		return snow
	case theme.ColorNamePlaceHolder:
		return secondary
	case theme.ColorNameSeparator:
		return stroke
	case fyne.ThemeColorName("inputBorder"):
		return stroke
	}
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}
func (chainTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }
func (chainTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (chainTheme) Size(n fyne.ThemeSizeName) float32 { return theme.DefaultTheme().Size(n) }

func txt(s string, fontSize float32, shade color.Color, weight bool) *canvas.Text {
	out := canvas.NewText(s, shade)
	out.TextSize = fontSize
	out.TextStyle = fyne.TextStyle{Bold: weight}
	return out
}

type insetLayout struct{ top, right, bottom, left float32 }

func (l insetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, obj := range objects {
		obj.Move(fyne.NewPos(l.left, l.top))
		obj.Resize(fyne.NewSize(max32(0, size.Width-l.left-l.right), max32(0, size.Height-l.top-l.bottom)))
	}
}
func (l insetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var min fyne.Size
	for _, obj := range objects {
		size := obj.MinSize()
		if size.Width > min.Width { min.Width = size.Width }
		if size.Height > min.Height { min.Height = size.Height }
	}
	return fyne.NewSize(min.Width+l.left+l.right, min.Height+l.top+l.bottom)
}
func pad(top, right, bottom, left float32, content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(insetLayout{top,right,bottom,left},content)
}
func max32(a,b float32) float32 { if a>b {return a};return b }

type fixedColumn struct{ width float32 }
func (l fixedColumn) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects { o.Move(fyne.NewPos(0,0));o.Resize(fyne.NewSize(l.width,size.Height)) }
}
func (l fixedColumn) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(l.width,1)
}

type splitCols struct { ratio, gap float32 }
func (l splitCols) Layout(objects []fyne.CanvasObject,size fyne.Size) {
	if len(objects)!=2 {return}
	available:=max32(0,size.Width-l.gap)
	left:=available*l.ratio
	objects[0].Move(fyne.NewPos(0,0))
	objects[0].Resize(fyne.NewSize(left,size.Height))
	objects[1].Move(fyne.NewPos(left+l.gap,0))
	objects[1].Resize(fyne.NewSize(available-left,size.Height))
}
func (l splitCols) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects)!=2 { return fyne.NewSize(0,0) }
	a,b:=objects[0].MinSize(),objects[1].MinSize()
	return fyne.NewSize(a.Width+b.Width+l.gap,max32(a.Height,b.Height))
}

func roundedPanel(background, outline color.Color, radius float32, body fyne.CanvasObject) fyne.CanvasObject {
	rect:=canvas.NewRectangle(background)
	rect.CornerRadius=radius
	rect.StrokeColor=outline
	rect.StrokeWidth=1.1
	return container.NewStack(rect,pad(18,20,18,20,body))
}
func standardCard(body ...fyne.CanvasObject) fyne.CanvasObject {
	return roundedPanel(slate,stroke,16,container.NewVBox(body...))
}

func imageIcon(name, svg string, dimension float32) fyne.CanvasObject {
	source:=fyne.NewStaticResource(name+".svg",[]byte(svg))
	img:=canvas.NewImageFromResource(source)
	img.FillMode=canvas.ImageFillContain
	return container.NewGridWrap(fyne.NewSize(dimension,dimension),img)
}

const linkSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<defs><linearGradient id="a" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#d6b5ff"/><stop offset="1" stop-color="#8941ed"/></linearGradient></defs>
<g fill="none" stroke="url(#a)" stroke-width="8" stroke-linecap="round" stroke-linejoin="round">
<path d="M23 40l18-18"/><path d="M38 13l4-4a12 12 0 0 1 17 17L45 40a12 12 0 0 1-17 0"/>
<path d="M26 51l-4 4A12 12 0 0 1 5 38l14-14a12 12 0 0 1 17 0"/>
</g></svg>`

const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 84 84">
<defs><linearGradient id="b" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#a876ff"/><stop offset="1" stop-color="#5422b3"/></linearGradient></defs>
<rect x="2" y="2" width="80" height="80" rx="21" fill="url(#b)"/>
<g fill="none" stroke="#ead6ff" stroke-width="8" stroke-linecap="round" stroke-linejoin="round">
<path d="M33 50l18-18"/><path d="M47 24l4-4a11 11 0 0 1 16 16L53 50a11 11 0 0 1-16 0"/>
<path d="M37 60l-4 4a11 11 0 0 1-16-16l14-14a11 11 0 0 1 16 0"/>
</g></svg>`

const serverSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<defs><linearGradient id="s" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#d8beff"/><stop offset="1" stop-color="#8651e9"/></linearGradient></defs>
<rect x="8" y="10" width="48" height="20" rx="6" fill="url(#s)" stroke="#b794ff" stroke-width="2"/>
<rect x="8" y="34" width="48" height="20" rx="6" fill="url(#s)" stroke="#b794ff" stroke-width="2"/>
<rect x="16" y="17" width="24" height="6" rx="3" fill="#473076"/>
<rect x="16" y="41" width="24" height="6" rx="3" fill="#473076"/>
<circle cx="47" cy="20" r="3.3" fill="#5ef0b7"/><circle cx="47" cy="44" r="3.3" fill="#5ef0b7"/>
</svg>`

const routerSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<defs><linearGradient id="r" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#ddc6ff"/><stop offset="1" stop-color="#8450dd"/></linearGradient></defs>
<g stroke="#b391fc" stroke-width="4" stroke-linecap="round"><path d="M18 35V8"/><path d="M47 35V8"/></g>
<rect x="5" y="32" width="54" height="22" rx="8" fill="url(#r)" stroke="#b391fc" stroke-width="2"/>
<circle cx="18" cy="43" r="3" fill="#4e307c"/><circle cx="30" cy="43" r="3" fill="#4e307c"/>
<rect x="42" y="40" width="10" height="5" rx="2.5" fill="#4e307c"/>
</svg>`

const arrowSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 32">
<g fill="none" stroke="#57e0b0" stroke-width="4.5" stroke-linecap="round" stroke-linejoin="round">
<path d="M5 16h36"/><path d="M29 5l12 11-12 11"/>
</g></svg>`

const shieldSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 54 62">
<path d="M27 5L48 12v16c0 13-8 22-21 29C14 50 6 41 6 28V12Z" fill="none" stroke="#69e4ad" stroke-width="5" stroke-linejoin="round"/>
<path d="M16 29l8 8 15-17" fill="none" stroke="#69e4ad" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`

// Local decorative image with soft gradient and violet sine-wave glow.
func sidebarBackground() fyne.CanvasObject {
	const W,H=360,1100
	img:=image.NewNRGBA(image.Rect(0,0,W,H))
	for y:=0;y<H;y++ {
		vy:=float64(y)/H
		for x:=0;x<W;x++ {
			vx:=float64(x)/W
			fog:=math.Exp(-math.Pow((vy-.88)/.21,2))*math.Exp(-math.Pow((vx+.12)/.8,2))*.38
			arc:=.82-.14*vx+.026*math.Sin(vx*5.1+1.1)
			arc2:=.74+.035*math.Sin(vx*4.6+1.5)-.04*vx
			d1:=vy-arc
			d2:=vy-arc2
			soft:=math.Exp(-d1*d1/(.045*.045))*.22 +math.Exp(-d2*d2/(.06*.06))*.12
			glow:=math.Exp(-d1*d1/(.003*.003))*.95+math.Exp(-d2*d2/(.0025*.0025))*.55
			t:=math.Min(1,fog+soft+glow)
			r:=float64(17)+t*130
			g:=float64(18)+t*62
			b:=float64(35)+t*170
			img.SetNRGBA(x,y,color.NRGBA{R:uint8(math.Min(255,r)),G:uint8(math.Min(255,g)),B:uint8(math.Min(255,b)),A:255})
		}
	}
	out:=canvas.NewImageFromImage(img)
	out.FillMode=canvas.ImageFillStretch
	return out
}

func buttonGradient() fyne.CanvasObject {
	const W,H=680,72
	img:=image.NewNRGBA(image.Rect(0,0,W,H))
	for y:=0;y<H;y++ {
		for x:=0;x<W;x++ {
			p:=float64(x)/W
			shine:=10*math.Exp(-math.Pow(float64(y)/13,2))
			r:=174-(58*p)+shine
			g:=111-(72*p)+shine
			b:=251-(27*p)+shine/2
			img.SetNRGBA(x,y,color.NRGBA{uint8(r),uint8(g),uint8(b),255})
		}
	}
	out:=canvas.NewImageFromImage(img)
	out.FillMode=canvas.ImageFillStretch
	return out
}

type mainAction struct {
	widget.BaseWidget
	OnTapped func()
	label *canvas.Text
	busy bool
}

func newMainAction(fn func()) *mainAction {
	b:=&mainAction{OnTapped:fn}
	b.ExtendBaseWidget(b)
	return b
}
func (b *mainAction) MinSize() fyne.Size { return fyne.NewSize(230,62) }
func (b *mainAction) CreateRenderer() fyne.WidgetRenderer {
	back:=canvas.NewRectangle(purple2)
	back.CornerRadius=14
	back.StrokeColor=pale
	back.StrokeWidth=1.3
	b.label=txt("▶  Запустить установку",20,snow,true)
	contents:=container.NewStack(back,pad(1,1,1,1,buttonGradient()),container.NewCenter(b.label))
	return widget.NewSimpleRenderer(contents)
}
func (b *mainAction) Tapped(*fyne.PointEvent) {
	if b.busy || b.OnTapped==nil { return }
	b.OnTapped()
}
func (b *mainAction) SetBusy(v bool) {
	b.busy=v
	if b.label==nil {return}
	if v {
		b.label.Text="Установка выполняется…"
		b.label.Color=secondary
	} else {
		b.label.Text="▶  Запустить установку"
		b.label.Color=snow
	}
	b.label.Refresh()
}

func stepRow(number, description string) fyne.CanvasObject {
	bubble:=canvas.NewCircle(purple2)
	bubble.StrokeColor=purple
	bubble.StrokeWidth=1
	n:=txt(number,14,snow,true)
	tag:=container.NewGridWrap(fyne.NewSize(31,31),container.NewStack(bubble,container.NewCenter(n)))
	item:=widget.NewLabel(description)
	item.Wrapping=fyne.TextWrapWord
	return container.NewBorder(nil,nil,tag,nil,pad(5,0,0,12,item))
}

func cardHeading(iconSVG, iconName, heading, description string) fyne.CanvasObject {
	iconBg:=canvas.NewRectangle(color.NRGBA{R:58,G:50,B:89,A:255})
	iconBg.CornerRadius=11
	iconBg.StrokeColor=stroke
	iconBg.StrokeWidth=1
	icon:=container.NewGridWrap(fyne.NewSize(50,50),container.NewStack(iconBg,container.NewCenter(imageIcon(iconName,iconSVG,33))))
	return container.NewHBox(icon,container.NewVBox(txt(heading,20,snow,true),txt(description,12,secondary,false)))
}

func labelledEntry(label string, e *widget.Entry) fyne.CanvasObject {
	border:=canvas.NewRectangle(color.Transparent)
	border.CornerRadius=9
	border.StrokeColor=stroke
	border.StrokeWidth=1
	input:=container.NewStack(e,border)
	return container.NewVBox(txt(label,13,snow,false),input)
}

func serverCard(title, subtitle string, host, port, user, password *widget.Entry) fyne.CanvasObject {
	return standardCard(
		cardHeading(serverSVG,"server",title,subtitle),
		container.NewGridWithColumns(2,
			labelledEntry("IPv4-адрес",host),
			labelledEntry("SSH-порт",port),
		),
		container.NewGridWithColumns(2,
			labelledEntry("Пользователь",user),
			labelledEntry("Пароль",password),
		),
	)
}

func statusCard(body ...fyne.CanvasObject) fyne.CanvasObject {
	return roundedPanel(color.NRGBA{R:21,G:47,B:45,A:255},color.NRGBA{R:56,G:178,B:124,A:255},18,container.NewVBox(body...))
}
