package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.friend.FriendLinkBackDTO;
import org.cuit.epoch.dto.friend.FriendLinkDTO;
import org.cuit.epoch.entity.FriendLink;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.FriendLinkVO;
import org.cuit.epoch.vo.page.PageResult;

import java.util.List;

public interface FriendLinkService extends IService<FriendLink> {

    /**
     * 查看友链列表
     *
     * @return 友链列表
     */
    List<FriendLinkDTO> listFriendLinks();

    /**
     * 查看后台友链列表
     *
     * @param condition 条件
     * @return 友链列表
     */
    PageResult<FriendLinkBackDTO> listFriendLinkDTO(ConditionVO condition);

    /**
     * 保存或更新友链
     *
     * @param friendLinkVO 友链
     */
    void saveOrUpdateFriendLink(FriendLinkVO friendLinkVO);

}
