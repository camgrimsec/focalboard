// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React, {useRef} from 'react'
import {useHotkeys} from 'react-hotkeys-hook'
import {useIntl} from 'react-intl'

import IconButton from '../widgets/buttons/iconButton'
import CloseIcon from '../widgets/icons/close'
import FullscreenIcon from '../widgets/icons/fullscreen'
import OptionsIcon from '../widgets/icons/options'
import MenuWrapper from '../widgets/menuWrapper'
import './dialog.scss'


type Props = {
    children: React.ReactNode
    toolsMenu?: React.ReactNode // some dialogs may not  require a toolmenu
    toolbar?: React.ReactNode
    hideCloseButton?: boolean
    className?: string
    title?: string
    showFullscreen?: boolean
    onClose: () => void,
    onToggleFullscreen?: () => void,
}

const Dialog = (props: Props) => {
    const {toolsMenu, toolbar, title} = props
    const intl = useIntl()

    const closeDialogText = intl.formatMessage({
        id: 'Dialog.closeDialog',
        defaultMessage: 'Close dialog',
    })

    useHotkeys('esc', () => props.onClose())

    const isBackdropClickedRef = useRef(false)

    return (
        <div className={`Dialog dialog-back ${props.className}`}>
            <div className='backdrop'/>
            <div
                className='wrapper'
                onClick={(e) => {
                    e.stopPropagation()
                    if(!isBackdropClickedRef.current){
                        return
                    }
                    isBackdropClickedRef.current = false
                    props.onClose()

                }}
                onMouseDown={(e) => {
                    if(e.target === e.currentTarget){
                        isBackdropClickedRef.current = true
                    }
                }}
            >
                <div
                    role='dialog'
                    className={`dialog ${props.showFullscreen ? 'fullscreen' : ''}`}
                >
                    <div className='toolbar'>
                        {title && <h1 className='text-heading5 mt-2'>{title}</h1>}
                        {
                            !props.hideCloseButton &&
                            <IconButton
                                onClick={props.onClose}
                                icon={<CloseIcon/>}
                                title={closeDialogText}
                                size='medium'
                            />
                        }
                        {toolbar && <div className='cardToolbar'>{toolbar}</div>}
                        <div className='toolbar--right'>
                            {toolbar && <div>{toolbar}</div>}
                            {props.onToggleFullscreen &&
                                <div style={{marginLeft: 'auto'}}>
                                    <IconButton
                                        onClick={props.onToggleFullscreen}
                                        icon={<FullscreenIcon/>}
                                        title={closeDialogText}
                                        size='medium'
                                    />
                                </div>
                            }
                            {toolsMenu && <MenuWrapper>
                                <IconButton
                                    size='medium'
                                    icon={<OptionsIcon/>}
                                />
                                {toolsMenu}
                            </MenuWrapper>
                            }
                        </div>
                    </div>
                    {props.children}
                </div>
            </div>
        </div>
    )
}

export default React.memo(Dialog)
